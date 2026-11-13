package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"os"
	"path"
	"strings"

	"github.com/romulospl/notify/services/ingest-web/internal/clientip"
	"github.com/romulospl/notify/services/ingest-web/internal/relay"
	"github.com/romulospl/notify/services/ingest-web/internal/site"
)

// maxBody é o tamanho máximo do corpo de um evento.
const maxBody = 16 << 10

// sender é a parte do relay.Client que o handler usa.
type sender interface {
	Send(ctx context.Context, ev relay.Event, userAgent string, truncatedIP netip.Addr) error
}

// limiter é a parte do limit.Limiter que o handler usa.
type limiter interface {
	Allow(key netip.Addr) bool
}

type gateway struct {
	sites   site.Store
	relay   sender
	limits  limiter
	ips     clientip.Resolver
	scripts *os.Root // nil desliga /s/
	log     *slog.Logger
}

func (g *gateway) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /e", g.event)
	mux.HandleFunc("OPTIONS /e", g.preflight)
	mux.HandleFunc("GET /healthz", healthz)
	if g.scripts != nil {
		mux.HandleFunc("GET /s/", g.script)
	}
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

// cors libera a chamada de qualquer origem. Nenhuma resposta usa cookie, então
// não há Allow-Credentials.
func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func (g *gateway) preflight(w http.ResponseWriter, _ *http.Request) {
	cors(w)
	h := w.Header()
	h.Set("Access-Control-Allow-Methods", "POST")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// event recebe o page view do script. O script manda o JSON como text/plain
// para não disparar preflight de CORS; application/json também é aceito.
func (g *gateway) event(w http.ResponseWriter, r *http.Request) {
	cors(w)

	ip, err := g.ips.FromRequest(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	truncated := clientip.Truncate(ip)
	if !g.limits.Allow(truncated) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "text/plain" && mt != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	ev, err := relay.Decode(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	active, err := g.sites.Active(r.Context(), ev.Payload.Website)
	if err != nil {
		g.log.Error("consulta à ingest_site falhou", "err", err)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	// Spec do E2, seção 6: site desconhecido ou que não aceita ingestão recebe a
	// mesma resposta do sucesso, para não dar pista a quem testa IDs, e o evento
	// é descartado.
	if !active {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := g.relay.Send(r.Context(), ev, r.UserAgent(), truncated); err != nil {
		g.log.Error("repasse ao umami falhou", "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// script serve os arquivos de SCRIPT_DIR. Cada versão fica em /s/vX.Y.Z/ e
// nunca muda, por isso o cache é de um ano e imutável. os.Root impede que um
// caminho saia da pasta.
func (g *gateway) script(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/s/")), "/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	f, err := g.scripts.Open(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			g.log.Warn("erro ao abrir script", "err", err)
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	cors(w)
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
