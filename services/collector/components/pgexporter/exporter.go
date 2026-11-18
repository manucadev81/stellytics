// Package pgexporter grava as métricas de API no Postgres (#31).
//
// Recebe traces e métricas já carimbados pelo tenantattr, extrai os pontos de
// 1 minuto com o pacote httpmetrics e faz o upsert em api_metric_1m (spec do
// E5, seção 4) com o usuário stellytics_collector. Nada além desses pontos é
// gravado: nenhum trace, span, log ou atributo de alta cardinalidade.
package pgexporter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/romulospl/stellytics/services/collector/components/httpmetrics"
)

// Type é o nome do exporter na configuração do coletor.
var Type = component.MustNewType("stellyticspg")

// Config do exporter.
type Config struct {
	// DatabaseURL aponta para o banco stellytics com o usuário stellytics_collector.
	DatabaseURL string `mapstructure:"database_url"`
}

// Validate implementa component.ConfigValidator.
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url é obrigatória")
	}
	return nil
}

// NewFactory cria a fábrica do exporter.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(Type, func() component.Config { return &Config{} },
		exporter.WithTraces(createTraces, component.StabilityLevelDevelopment),
		exporter.WithMetrics(createMetrics, component.StabilityLevelDevelopment),
	)
}

// Um exporter só por configuração, compartilhado pelos pipelines de traces e
// de métricas: a D9 (métrica tem prioridade sobre span) precisa do mesmo estado.
var shared = map[*Config]*pgExporter{}

func get(set exporter.Settings, cfg *Config) *pgExporter {
	if e, ok := shared[cfg]; ok {
		return e
	}
	e := &pgExporter{cfg: cfg, logger: set.Logger, agg: httpmetrics.New()}
	shared[cfg] = e
	return e
}

func createTraces(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Traces, error) {
	e := get(set, cfg.(*Config))
	return exporterhelper.NewTraces(ctx, set, cfg, e.pushTraces, e.options()...)
}

func createMetrics(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Metrics, error) {
	e := get(set, cfg.(*Config))
	return exporterhelper.NewMetrics(ctx, set, cfg, e.pushMetrics, e.options()...)
}

// writer grava um lote. O do Postgres é o pgWriter; os testes usam outro.
type writer interface {
	write(ctx context.Context, b httpmetrics.Batch) error
}

type pgExporter struct {
	cfg    *Config
	logger *zap.Logger
	agg    *httpmetrics.Aggregator

	started int // quantos pipelines chamaram Start
	pool    *pgxpool.Pool
	w       writer
	stop    context.CancelFunc
}

// As novas tentativas ficam aqui, sobre o lote já calculado: recalcular a
// partir do OTLP daria delta zero para as séries cumulativas. Por isso a
// retentativa do exporterhelper fica desligada.
func (e *pgExporter) options() []exporterhelper.Option {
	return []exporterhelper.Option{
		exporterhelper.WithStart(e.start),
		exporterhelper.WithShutdown(e.shutdown),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithRetry(disabledRetry()),
	}
}

func (e *pgExporter) start(ctx context.Context, _ component.Host) error {
	e.started++
	if e.started > 1 {
		return nil
	}
	pool, err := pgxpool.New(ctx, e.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("stellyticspg: database_url: %w", err)
	}
	e.pool = pool
	e.w = pgWriter{pool: pool}
	sweepCtx, cancel := context.WithCancel(context.Background())
	e.stop = cancel
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-t.C:
				e.agg.Sweep()
			}
		}
	}()
	return nil
}

func (e *pgExporter) shutdown(context.Context) error {
	e.started--
	if e.started > 0 {
		return nil
	}
	if e.stop != nil {
		e.stop()
	}
	if e.pool != nil {
		e.pool.Close()
	}
	for cfg, x := range shared {
		if x == e {
			delete(shared, cfg)
		}
	}
	return nil
}

func (e *pgExporter) pushTraces(ctx context.Context, td ptrace.Traces) error {
	return e.save(ctx, e.agg.Traces(td))
}

func (e *pgExporter) pushMetrics(ctx context.Context, md pmetric.Metrics) error {
	return e.save(ctx, e.agg.Metrics(md))
}

// save grava o lote com até 4 tentativas. Se todas falharem, o lote é
// descartado com erro permanente, para ninguém recalcular a partir do OTLP.
func (e *pgExporter) save(ctx context.Context, b httpmetrics.Batch) error {
	if len(b) == 0 {
		return nil
	}
	var err error
	for attempt, wait := 0, 200*time.Millisecond; attempt < 4; attempt, wait = attempt+1, wait*2 {
		if err = e.w.write(ctx, b); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			attempt = 4
		case <-time.After(wait):
		}
	}
	e.logger.Error("lote de métricas de API descartado", zap.Int("linhas", len(b)), zap.Error(err))
	return consumererror.NewPermanent(err)
}

// upsert soma o ponto à linha, se ela já existir (spec, seção 4).
const upsert = `
INSERT INTO api_metric_1m AS m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                                request_count, duration_sum, duration_buckets)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (tenant_id, api_service_id, route, method, status_code, bucket_start) DO UPDATE SET
  request_count = m.request_count + EXCLUDED.request_count,
  duration_sum = m.duration_sum + EXCLUDED.duration_sum,
  duration_buckets = api_metric_add_buckets(m.duration_buckets, EXCLUDED.duration_buckets)`

type pgWriter struct{ pool *pgxpool.Pool }

// write grava o lote numa transação, em ordem fixa de chave, para dois lotes
// concorrentes não travarem um ao outro.
func (w pgWriter) write(ctx context.Context, b httpmetrics.Batch) error {
	keys := make([]httpmetrics.Key, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compareKeys)

	return pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, k := range keys {
			p := b[k]
			batch.Queue(upsert, k.Bucket, k.TenantID, k.ServiceID, k.Route, k.Method, k.Status,
				p.Count, p.Sum, p.Buckets[:])
		}
		return tx.SendBatch(ctx, batch).Close()
	})
}

func compareKeys(a, b httpmetrics.Key) int {
	return cmpChain(
		strings.Compare(a.TenantID, b.TenantID),
		strings.Compare(a.ServiceID, b.ServiceID),
		strings.Compare(a.Route, b.Route),
		strings.Compare(a.Method, b.Method),
		int(a.Status)-int(b.Status),
		a.Bucket.Compare(b.Bucket),
	)
}

func cmpChain(cmps ...int) int {
	for _, c := range cmps {
		if c != 0 {
			return c
		}
	}
	return 0
}

func disabledRetry() configretry.BackOffConfig {
	c := configretry.NewDefaultBackOffConfig()
	c.Enabled = false
	return c
}
