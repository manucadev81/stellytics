// replication-monitor expõe apenas métricas agregadas do slot físico configurado.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type sample struct {
	slotExists, streaming   int
	lagBytes, retainedBytes int64
}
type collectFunc func(context.Context) (sample, error)

func metrics(collect collectFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		s, err := collect(ctx)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if err != nil {
			fmt.Fprintln(w, "stellytics_replication_scrape_success 0")
			return
		}
		fmt.Fprintf(w, "stellytics_replication_scrape_success 1\nstellytics_replication_slot_exists %d\nstellytics_replication_streaming %d\nstellytics_replication_lag_bytes %d\nstellytics_replication_retained_wal_bytes %d\n", s.slotExists, s.streaming, s.lagBytes, s.retainedBytes)
	}
}

func collector(config *pgx.ConnConfig, slot string) collectFunc {
	return func(ctx context.Context) (sample, error) {
		var s sample
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			return s, err
		}
		defer conn.Close(context.Background())
		err = conn.QueryRow(ctx, `SELECT
    CASE WHEN s.slot_name IS NULL THEN 0 ELSE 1 END,
    CASE WHEN r.state = 'streaming' AND r.replay_lsn IS NOT NULL THEN 1 ELSE 0 END,
    COALESCE(GREATEST(pg_wal_lsn_diff(pg_current_wal_lsn(), r.replay_lsn), 0), 0)::bigint,
    COALESCE(GREATEST(pg_wal_lsn_diff(pg_current_wal_lsn(), s.restart_lsn), 0), 0)::bigint
   FROM (SELECT 1) AS one
   LEFT JOIN pg_replication_slots s ON s.slot_name = $1 AND s.slot_type = 'physical'
   LEFT JOIN pg_stat_replication r ON r.pid = s.active_pid`, slot).Scan(&s.slotExists, &s.streaming, &s.lagBytes, &s.retainedBytes)
		return s, err
	}
}

func main() {
	slot := os.Getenv("REPLICATION_SLOT")
	if slot == "" {
		log.Fatal("REPLICATION_SLOT is required")
	}
	config, err := pgx.ParseConfig("")
	if err != nil {
		log.Fatal("invalid postgres configuration")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", metrics(collector(config, slot)))
	srv := &http.Server{Addr: ":9187", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
