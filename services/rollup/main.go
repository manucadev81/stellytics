// Comando rollup: job de consolidação e retenção das métricas de API (#34).
//
// A cada ROLLUP_INTERVAL chama api_metric_maintain(ROLLUP_LOOKBACK) no banco
// stellytics, que cria as partições futuras, consolida api_metric_1m em
// api_metric_1h e api_metric_1d e apaga as partições vencidas (spec do E5,
// seção 5). Toda a lógica fica na migração 0005 do portal; este processo só
// agenda. Expõe GET /healthz, que responde 200 enquanto a última execução
// bem-sucedida tiver no máximo dois intervalos.
//
// Com UMAMI_DATABASE_URL definida, a mesma execução também aplica a retenção
// dos eventos web no banco umami (#154): apaga eventos e sessões mais antigos
// que UMAMI_RETENTION_DAYS (padrão 183, cerca de seis meses). O Umami 3.4.1
// não tem configuração de retenção própria; quem poda é este job.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type config struct {
	DatabaseURL string
	ListenAddr  string
	Interval    time.Duration
	Lookback    time.Duration
	// Banco e retenção do Umami. Sem UMAMI_DATABASE_URL a poda fica desligada
	// (o resto do job segue normal).
	UmamiDatabaseURL   string
	UmamiRetentionDays int
}

// Padrão da retenção dos eventos do Umami, em dias: cerca de seis meses.
// Valor e justificativa em docs/umami.md e docs/privacidade.md (seção 6).
const defaultUmamiRetentionDays = 183

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		DatabaseURL:        getenv("DATABASE_URL"),
		ListenAddr:         getenv("LISTEN_ADDR"),
		Interval:           15 * time.Minute,
		Lookback:           48 * time.Hour,
		UmamiDatabaseURL:   getenv("UMAMI_DATABASE_URL"),
		UmamiRetentionDays: defaultUmamiRetentionDays,
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL vazia")
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	for name, dst := range map[string]*time.Duration{"ROLLUP_INTERVAL": &c.Interval, "ROLLUP_LOOKBACK": &c.Lookback} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return c, fmt.Errorf("%s inválida: %q", name, v)
			}
			*dst = d
		}
	}
	if c.Lookback < 2*time.Hour {
		return c, errors.New("ROLLUP_LOOKBACK precisa ser de pelo menos 2h")
	}
	if v := getenv("UMAMI_RETENTION_DAYS"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 0 {
			return c, fmt.Errorf("UMAMI_RETENTION_DAYS inválida: %q", v)
		}
		c.UmamiRetentionDays = d
	}
	if c.UmamiDatabaseURL == "" && getenv("UMAMI_RETENTION_DAYS") != "" {
		return c, errors.New("UMAMI_RETENTION_DAYS sem UMAMI_DATABASE_URL não tem efeito")
	}
	return c, nil
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "consulta o /healthz local e sai (para o healthcheck do contêiner)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if *healthcheck {
		os.Exit(checkHealth(os.Getenv("LISTEN_ADDR")))
	}
	if err := run(log); err != nil {
		log.Error("rollup parou", "err", err)
		os.Exit(1)
	}
}

// maintain roda uma execução do job.
func maintain(ctx context.Context, pool *pgxpool.Pool, lookback time.Duration) error {
	_, err := pool.Exec(ctx, "SELECT api_metric_maintain(make_interval(secs => $1))", lookback.Seconds())
	return err
}

// pruneUmami aplica a retenção dos eventos web (#154): apaga os eventos mais
// antigos que a retenção e depois as sessões da mesma idade que não sustentam
// mais nenhum evento — a sessão de um visitante que continua voltando fica,
// para que os eventos recentes dela continuem com navegador, sistema e
// localização. O schema do Umami 3.4.1 não tem chaves estrangeiras, então a
// ordem é só uma escolha; duas execuções no mesmo instante são idempotentes.
// Devolve eventos e sessões apagados.
func pruneUmami(ctx context.Context, pool *pgxpool.Pool, days int) (events, sessions int64, err error) {
	tag, err := pool.Exec(ctx, "DELETE FROM website_event WHERE created_at < now() - make_interval(days => $1)", days)
	if err != nil {
		return 0, 0, fmt.Errorf("website_event: %w", err)
	}
	events = tag.RowsAffected()
	tag, err = pool.Exec(ctx, `DELETE FROM session s
		WHERE s.created_at < now() - make_interval(days => $1)
		  AND NOT EXISTS (SELECT 1 FROM website_event e WHERE e.session_id = s.session_id)`, days)
	if err != nil {
		return events, 0, fmt.Errorf("session: %w", err)
	}
	return events, tag.RowsAffected(), nil
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	defer pool.Close()

	// Pool do banco umami, só quando a retenção está ligada.
	var umamiPool *pgxpool.Pool
	if cfg.UmamiDatabaseURL != "" && cfg.UmamiRetentionDays > 0 {
		umamiPool, err = pgxpool.New(ctx, cfg.UmamiDatabaseURL)
		if err != nil {
			return fmt.Errorf("UMAMI_DATABASE_URL: %w", err)
		}
		defer umamiPool.Close()
		log.Info("retenção do Umami no ar", "days", cfg.UmamiRetentionDays)
	} else {
		log.Info("retenção do Umami desligada")
	}

	var lastOK atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if time.Since(time.Unix(0, lastOK.Load())) > 2*cfg.Interval {
			http.Error(w, "sem execução recente", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	defer srv.Close()

	log.Info("rollup no ar", "interval", cfg.Interval.String(), "lookback", cfg.Lookback.String())
	for {
		start := time.Now()
		runCtx, cancel := context.WithTimeout(ctx, cfg.Interval)
		err := maintain(runCtx, pool, cfg.Lookback)
		cancel()
		if err != nil {
			log.Error("execução falhou", "err", err)
		} else {
			lastOK.Store(time.Now().UnixNano())
			log.Info("execução concluída", "duration", time.Since(start).String())
			if umamiPool != nil {
				umamiCtx, umamiCancel := context.WithTimeout(ctx, cfg.Interval)
				events, sessions, umamiErr := pruneUmami(umamiCtx, umamiPool, cfg.UmamiRetentionDays)
				umamiCancel()
				if umamiErr != nil {
					log.Error("retenção do Umami falhou", "err", umamiErr)
				} else {
					log.Info("retenção do Umami concluída", "events", events, "sessions", sessions)
				}
			}
		}
		next := time.NewTimer(nextRun(cfg.Interval, err != nil))
		select {
		case <-ctx.Done():
			next.Stop()
			return nil
		case err := <-errc:
			next.Stop()
			return err
		case <-next.C:
		}
	}
}

// retryDelay é a espera depois de uma execução que falhou.
const retryDelay = 30 * time.Second

// nextRun diz quanto esperar até a próxima execução. Depois de uma falha (o
// banco reiniciando, as migrações ainda não aplicadas) tenta de novo logo, em
// vez de ficar um intervalo inteiro sem consolidar e com o healthcheck
// vermelho. O job é idempotente, então repetir não tem custo.
func nextRun(interval time.Duration, failed bool) time.Duration {
	if failed && retryDelay < interval {
		return retryDelay
	}
	return interval
}

// checkHealth faz o papel de curl/wget na imagem sem shell.
func checkHealth(listen string) int {
	if listen == "" {
		listen = ":8080"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
