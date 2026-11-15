package main

// Testes do limite de repasses simultâneos ao Umami (RELAY_MAX_INFLIGHT).

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/romulospl/notify/services/ingest-web/internal/fingerprint"
	"github.com/romulospl/notify/services/ingest-web/internal/relay"
)

// slowUmami segura cada /api/send até release fechar.
type slowUmami struct {
	release chan struct{}
	once    sync.Once
	started atomic.Int64
	cur     atomic.Int64
	peak    atomic.Int64
}

func newSlowUmami(t *testing.T) (*slowUmami, relay.Sender) {
	t.Helper()
	u := &slowUmami{release: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.started.Add(1)
		n := u.cur.Add(1)
		for {
			p := u.peak.Load()
			if n <= p || u.peak.CompareAndSwap(p, n) {
				break
			}
		}
		defer u.cur.Add(-1)
		select {
		case <-u.release:
		case <-r.Context().Done():
		}
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	return u, relay.NewClient(base, "x-notify-client-ip", srv.Client())
}

func (u *slowUmami) open() {
	u.once.Do(func() { close(u.release) })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condição não atingida")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func postAsync(s setup, body string, ua string) <-chan *http.Response {
	ch := make(chan *http.Response, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, s.server.URL+"/e", strings.NewReader(body))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("User-Agent", ua)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- nil
			return
		}
		resp.Body.Close()
		ch <- resp
	}()
	return ch
}

func TestSaturationReturns503AndKeepsOtherRoutes(t *testing.T) {
	umami, inner := newSlowUmami(t)
	limited := relay.NewLimited(inner, 2, 20*time.Millisecond)
	s := newSetup(t, func(g *gateway) { g.relay = limited })
	t.Cleanup(umami.open) // roda antes do Close do gateway, para uma falha não travar

	a := postAsync(s, eventBody(activeSite), "ua-a")
	b := postAsync(s, eventBody(activeSite), "ua-b")
	waitFor(t, func() bool { return umami.started.Load() == 2 })

	resp := post(t, s, "text/plain", eventBody(activeSite), "User-Agent", "ua-c")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("sem Retry-After")
	}
	if limited.Dropped() != 1 || umami.started.Load() != 2 {
		t.Errorf("descartados = %d, umami recebeu %d", limited.Dropped(), umami.started.Load())
	}

	// O limite vale só para o repasse: prova inválida, /c, /healthz e site
	// recusado respondem como sempre, mesmo com o repasse saturado.
	bad := strings.Replace(eventBody(activeSite), `"n":"`, `"n":"x`, 1)
	if r := post(t, s, "text/plain", bad); r.StatusCode != http.StatusNoContent {
		t.Errorf("prova inválida: %d", r.StatusCode)
	}
	if r := post(t, s, "text/plain", eventBody(pendingSite)); r.StatusCode != http.StatusNoContent {
		t.Errorf("site recusado: %d", r.StatusCode)
	}
	for _, p := range []string{"/c?s=" + activeSite, "/healthz"} {
		r, err := http.Get(s.server.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
	if limited.Dropped() != 1 {
		t.Errorf("descartados = %d, quero 1 (as outras rotas não contam)", limited.Dropped())
	}

	umami.open()
	for _, ch := range []<-chan *http.Response{a, b} {
		if r := <-ch; r == nil || r.StatusCode != http.StatusNoContent {
			t.Fatalf("aceitos deveriam dar 204: %v", r)
		}
	}
	// Livre de novo.
	if r := post(t, s, "text/plain", eventBody(activeSite), "User-Agent", "ua-d"); r.StatusCode != http.StatusNoContent {
		t.Fatalf("depois da saturação: %d", r.StatusCode)
	}
}

// Um evento descartado por saturação não gasta a cota de fingerprints do IP:
// com limite 1, depois de recusado ele pode ser reenviado e a fingerprint
// ainda cabe; uma segunda, diferente, só é recusada pela cota quando a
// primeira realmente passou.
func TestSaturationDoesNotConsumeFingerprintQuota(t *testing.T) {
	umami, inner := newSlowUmami(t)
	limited := relay.NewLimited(inner, 1, 10*time.Millisecond)
	s := newSetup(t, func(g *gateway) {
		g.relay = limited
		g.fps = fingerprint.New(3, 10*time.Minute)
	})
	t.Cleanup(umami.open)
	// Ocupa a única vaga (conta 1 das 3 fingerprints do IP).
	postAsync(s, eventBody(activeSite), "ua-hold")
	waitFor(t, func() bool { return umami.started.Load() == 1 })
	// Mesmo IP: três fingerprints novas, todas recusadas por saturação. Se cada
	// uma gastasse a cota (limite 3, com a do hold), a terceira cairia na cota (204).
	for _, ua := range []string{"ua-1", "ua-2", "ua-3"} {
		r := post(t, s, "text/plain", eventBody(activeSite), "User-Agent", ua)
		if r.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, quero 503", ua, r.StatusCode)
		}
	}
	umami.open()
	waitFor(t, func() bool { return limited.InFlight() == 0 })
	// Agora há vaga: as fingerprints refugadas ainda cabem na cota.
	for _, ua := range []string{"ua-1", "ua-2"} {
		if r := post(t, s, "text/plain", eventBody(activeSite), "User-Agent", ua); r.StatusCode != http.StatusNoContent {
			t.Fatalf("%s: %d", ua, r.StatusCode)
		}
	}
	if n := umami.started.Load(); n != 3 {
		t.Fatalf("umami recebeu %d, quero 3", n)
	}
}

// Sem RELAY_MAX_INFLIGHT (relay sem o limitador), o comportamento é o de antes:
// 30 repasses lentos correm juntos, nenhum é recusado.
func TestNoLimitKeepsUnboundedRelay(t *testing.T) {
	umami, inner := newSlowUmami(t)
	s := newSetup(t, func(g *gateway) { g.relay = inner })
	t.Cleanup(umami.open)
	var chans []<-chan *http.Response
	for i := range 30 {
		chans = append(chans, postAsync(s, eventBody(activeSite), "ua-"+string(rune('a'+i))))
	}
	waitFor(t, func() bool { return umami.started.Load() == 30 })
	if umami.peak.Load() != 30 {
		t.Errorf("pico = %d, quero 30 (sem limite)", umami.peak.Load())
	}
	umami.open()
	for _, ch := range chans {
		if r := <-ch; r == nil || r.StatusCode != http.StatusNoContent {
			t.Fatalf("resposta %v", r)
		}
	}
}

func TestPanicInRelayFreesSlot(t *testing.T) {
	var boom atomic.Bool
	boom.Store(true)
	inner := panicSender(func() error {
		if boom.Load() {
			panic("boom")
		}
		return nil
	})
	limited := relay.NewLimited(inner, 1, 10*time.Millisecond)
	s := newSetup(t, func(g *gateway) { g.relay = limited })
	if r := post(t, s, "text/plain", eventBody(activeSite)); r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("panic: %d", r.StatusCode)
	}
	boom.Store(false)
	if r := post(t, s, "text/plain", eventBody(activeSite), "User-Agent", "outro"); r.StatusCode != http.StatusNoContent {
		t.Fatalf("depois do panic: %d (vaga presa?)", r.StatusCode)
	}
}

type panicSender func() error

func (f panicSender) Send(context.Context, relay.Event, string, netip.Addr) error { return f() }

func TestLogSaturatedReportsCountOnlyOnChange(t *testing.T) {
	g := newSlowGate()
	limited := relay.NewLimited(g, 1, 0)
	go func() { _ = limited.Send(context.Background(), relay.Event{}, "ua", netip.Addr{}) }()
	<-g.in
	_ = limited.Send(context.Background(), relay.Event{Payload: relay.Payload{Website: activeSite}}, "Mozilla-secreto", netip.MustParseAddr("203.0.113.9"))

	var buf syncBuffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { logSaturated(ctx, log, limited, 10*time.Millisecond); close(done) }()
	waitFor(t, func() bool { return strings.Contains(buf.String(), "saturação") })
	time.Sleep(60 * time.Millisecond) // vários ticks sem mudança: nenhuma linha nova
	cancel()
	<-done
	out := buf.String()
	if strings.Count(out, "saturação") != 1 {
		t.Fatalf("esperava uma linha só: %s", out)
	}
	if !strings.Contains(out, `"descartados":1`) || !strings.Contains(out, `"total":1`) {
		t.Fatalf("log inesperado: %s", out)
	}
	for _, secret := range []string{activeSite, "Mozilla-secreto", "203.0.113.9"} {
		if strings.Contains(out, secret) {
			t.Fatalf("o log não pode conter %q", secret)
		}
	}
	close(g.out)
}

type slowGate struct {
	in  chan struct{}
	out chan struct{}
}

func newSlowGate() *slowGate { return &slowGate{in: make(chan struct{}, 1), out: make(chan struct{})} }

func (g *slowGate) Send(ctx context.Context, _ relay.Event, _ string, _ netip.Addr) error {
	g.in <- struct{}{}
	select {
	case <-g.out:
	case <-ctx.Done():
	}
	return nil
}
