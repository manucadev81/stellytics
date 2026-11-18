package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romulospl/stellytics/services/internal/pgtest"
)

func TestLoadConfig(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://x"}
	get := func(k string) string { return env[k] }
	c, err := loadConfig(get)
	if err != nil || c.Interval != 15*time.Minute || c.Lookback != 48*time.Hour || c.ListenAddr != ":8080" {
		t.Fatalf("padrões: %+v, %v", c, err)
	}
	for k, v := range map[string]string{"ROLLUP_INTERVAL": "abc", "ROLLUP_LOOKBACK": "1h"} {
		env[k] = v
		if _, err := loadConfig(get); err == nil {
			t.Errorf("%s=%s deveria falhar", k, v)
		}
		delete(env, k)
	}
	delete(env, "DATABASE_URL")
	if _, err := loadConfig(get); err == nil {
		t.Error("sem DATABASE_URL deveria falhar")
	}
}

// Consolida um ponto de 1 minuto de duas horas atrás pela mesma chamada do serviço, duas vezes.
func TestNextRun(t *testing.T) {
	for _, c := range []struct {
		interval time.Duration
		failed   bool
		want     time.Duration
	}{
		{15 * time.Minute, false, 15 * time.Minute},
		{15 * time.Minute, true, retryDelay},
		{10 * time.Second, true, 10 * time.Second},
	} {
		if got := nextRun(c.interval, c.failed); got != c.want {
			t.Errorf("nextRun(%s, %v) = %s, quero %s", c.interval, c.failed, got, c.want)
		}
	}
}

func TestMaintain(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL não definida")
	}
	ctx := context.Background()
	adminURL = pgtest.Database(t, adminURL)
	pool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID, serviceID := pgtest.NewUUID(t), pgtest.NewUUID(t)
	t.Cleanup(func() {
		for _, tbl := range []string{"api_metric_1m", "api_metric_1h", "api_metric_1d"} {
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+tbl+" WHERE tenant_id = $1", tenantID)
		}
	})
	at := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	_, err = pool.Exec(ctx, `INSERT INTO api_metric_1m (bucket_start, tenant_id, api_service_id, route, method,
		status_code, request_count, duration_sum, duration_buckets)
		VALUES ($1, $2, $3, '/r', 'GET', 200, 3, 0.3, '{0,0,0,3,0,0,0,0,0,0,0,0,0,0,0}')`, at, tenantID, serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := maintain(ctx, pool, 48*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	for _, tbl := range []string{"api_metric_1h", "api_metric_1d"} {
		var n int64
		if err := pool.QueryRow(ctx, "SELECT sum(request_count) FROM "+tbl+" WHERE tenant_id = $1", tenantID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Errorf("%s: %d requisições, esperava 3", tbl, n)
		}
	}
}
