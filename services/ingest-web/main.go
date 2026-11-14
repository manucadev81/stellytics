// Comando ingest-web: gateway entre o script de rastreamento e o Umami.
//
// Recebe os eventos em POST /e, confere o websiteId na visão ingest_site,
// trunca o IP e repassa ao /api/send do Umami pela rede interna. Também serve
// o script em /s/vX.Y.Z/ e expõe /healthz. A configuração vem do ambiente
// (ver internal/config e o README).
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romulospl/notify/services/ingest-web/internal/clientip"
	"github.com/romulospl/notify/services/ingest-web/internal/config"
	"github.com/romulospl/notify/services/ingest-web/internal/limit"
	"github.com/romulospl/notify/services/ingest-web/internal/proof"
	"github.com/romulospl/notify/services/ingest-web/internal/relay"
	"github.com/romulospl/notify/services/ingest-web/internal/site"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "consulta o /healthz local e sai (para o healthcheck do contêiner)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if *healthcheck {
		os.Exit(checkHealth(os.Getenv("LISTEN_ADDR")))
	}
	if err := run(log); err != nil {
		log.Error("gateway parou", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
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

	var scripts *os.Root
	if cfg.ScriptDir != "" {
		if scripts, err = os.OpenRoot(cfg.ScriptDir); err != nil {
			return fmt.Errorf("SCRIPT_DIR: %w", err)
		}
		defer scripts.Close()
	}

	secret := cfg.ProofSecret
	if secret == nil {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
		log.Warn("PROOF_SECRET vazio: chave sorteada, tokens não valem entre reinícios nem entre réplicas")
	}

	limits := limit.New(cfg.RateLimit, cfg.RateBurst, 10*time.Minute)
	go limits.Run(ctx, time.Minute)

	g := &gateway{
		sites:   site.NewCached(site.PG{Pool: pool}, cfg.SiteCacheTTL),
		relay:   relay.NewClient(cfg.UmamiURL, cfg.ClientIPHeader, &http.Client{Timeout: 10 * time.Second}),
		limits:  limits,
		ips:     clientip.Resolver{Trusted: cfg.TrustedProxies},
		proofs:  proof.New(secret, cfg.ProofTTL, cfg.ProofBits),
		scripts: scripts,
		log:     log,
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           g.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("gateway no ar", "addr", cfg.ListenAddr, "scripts", cfg.ScriptDir != "")

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
