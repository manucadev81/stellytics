package main

// Testes da #24: o gateway não grava cookie nem registra IP ou User-Agent.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/romulospl/stellytics/services/ingest-web/internal/clientip"
	"github.com/romulospl/stellytics/services/ingest-web/internal/relay"
)

// clientIP é o IP que o "Caddy" do teste informa no X-Forwarded-For.
const clientIP = "203.0.113.77"

type failingSites struct{}

func (failingSites) Active(context.Context, string) (bool, error) {
	return false, errors.New("banco fora do ar")
}

type panicSites struct{}

func (panicSites) Active(context.Context, string) (bool, error) { panic("estado inesperado") }

// syncBuffer deixa o log ser lido enquanto o servidor ainda escreve.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogsWithoutIPOrUserAgent(t *testing.T) {
	brokenUmami := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(brokenUmami.Close)
	base, _ := url.Parse(brokenUmami.URL)

	cases := map[string]func(*gateway){
		"umami falha": func(g *gateway) {
			g.relay = relay.NewClient(base, "x-stellytics-client-ip", brokenUmami.Client())
		},
		"banco falha": func(g *gateway) { g.sites = failingSites{} },
		"panic":       func(g *gateway) { g.sites = panicSites{} },
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			var logs syncBuffer
			s := newSetup(t, opt, func(g *gateway) {
				g.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				g.ips = clientip.Resolver{Trusted: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
			})
			resp := post(t, s, "text/plain", eventBody(activeSite), "X-Forwarded-For", clientIP)
			if resp.StatusCode < 500 {
				t.Fatalf("status = %d, quero erro do servidor", resp.StatusCode)
			}
			out := logs.String()
			if out == "" {
				t.Fatal("a falha não foi registrada")
			}
			for _, secret := range []string{clientIP, "203.0.113.0", "127.0.0.1", browserUA, "Mozilla"} {
				if strings.Contains(out, secret) {
					t.Errorf("log contém %q:\n%s", secret, out)
				}
			}
		})
	}
}

func TestNoCookies(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "v1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "v1.0.0", "n.js"), []byte("void 0"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	s := newSetup(t, func(g *gateway) { g.scripts = root })

	requests := []struct{ method, path, contentType, body string }{
		{http.MethodPost, "/e", "text/plain", eventBody(activeSite)},
		{http.MethodPost, "/e", "text/plain", eventBody(pendingSite)},
		{http.MethodPost, "/e", "text/plain", "{"},
		{http.MethodPost, "/e", "image/png", ""},
		{http.MethodOptions, "/e", "", ""},
		{http.MethodGet, "/s/v1.0.0/n.js", "", ""},
		{http.MethodGet, "/s/v9.9.9/n.js", "", ""},
		{http.MethodGet, "/healthz", "", ""},
		{http.MethodGet, "/nada", "", ""},
	}
	for _, r := range requests {
		req, _ := http.NewRequest(r.method, s.server.URL+r.path, strings.NewReader(r.body))
		if r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		// Um cookie que o navegador mandasse não pode voltar nem ser repassado.
		req.Header.Set("Cookie", "sessao=abc")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if c := resp.Header.Values("Set-Cookie"); len(c) > 0 {
			t.Errorf("%s %s (%d) grava cookie: %q", r.method, r.path, resp.StatusCode, c)
		}
	}
	for _, rec := range s.umami.received() {
		if c := rec.header.Get("Cookie"); c != "" {
			t.Errorf("cookie repassado ao Umami: %q", c)
		}
	}
}
