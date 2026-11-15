package site

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

type countingStore struct {
	calls  int
	active map[string]bool
	err    error
}

func (s *countingStore) Active(_ context.Context, id string) (bool, error) {
	s.calls++
	return s.active[id], s.err
}

func TestCachedKeepsPositiveAndNegative(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	next := &countingStore{active: map[string]bool{"a": true}}
	c := NewCached(next, time.Minute)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	for range 3 {
		if ok, _ := c.Active(ctx, "a"); !ok {
			t.Fatal("a deveria estar ativo")
		}
		if ok, _ := c.Active(ctx, "b"); ok {
			t.Fatal("b não deveria estar ativo")
		}
	}
	if next.calls != 2 {
		t.Fatalf("consultas = %d, quero 2", next.calls)
	}

	// Depois do TTL, a resposta é buscada de novo.
	next.active["b"] = true
	now = now.Add(time.Minute)
	if ok, _ := c.Active(ctx, "b"); !ok {
		t.Fatal("b não foi atualizado depois do TTL")
	}
	if next.calls != 3 {
		t.Fatalf("consultas = %d, quero 3", next.calls)
	}
}

func TestCachedDoesNotKeepErrors(t *testing.T) {
	next := &countingStore{err: errors.New("banco fora")}
	c := NewCached(next, time.Minute)
	ctx := context.Background()
	for range 2 {
		if _, err := c.Active(ctx, "a"); err == nil {
			t.Fatal("erro não repassado")
		}
	}
	if next.calls != 2 {
		t.Fatalf("consultas = %d, quero 2", next.calls)
	}
}

func TestCachedIsBounded(t *testing.T) {
	c := NewCached(&countingStore{}, time.Hour)
	ctx := context.Background()
	for i := range maxCached + 10 {
		_, _ = c.Active(ctx, string(rune(i)))
	}
	if n := len(c.neg); n > maxCached {
		t.Fatalf("cache com %d entradas, limite %d", n, maxCached)
	}
}

// flakyStore troca entre respostas e falhas e é seguro para uso concorrente.
type flakyStore struct {
	mu      sync.Mutex
	calls   int
	active  map[string]bool
	fail    bool
	gate    chan struct{} // se não nulo, Active espera nele (ou no ctx)
	started chan struct{} // se não nulo, recebe um sinal a cada consulta iniciada
}

func (s *flakyStore) Active(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	s.calls++
	fail, a, gate, started := s.fail, s.active[id], s.gate, s.started
	s.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	if fail {
		return false, errors.New("banco fora")
	}
	return a, nil
}

func (s *flakyStore) set(fail bool) {
	s.mu.Lock()
	s.fail = fail
	s.mu.Unlock()
}

func (s *flakyStore) setGate(g chan struct{}) {
	s.mu.Lock()
	s.gate = g
	s.mu.Unlock()
}

func (s *flakyStore) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// testClock é um relógio controlado, seguro para uso concorrente.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (k *testClock) now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.t
}

func (k *testClock) add(d time.Duration) {
	k.mu.Lock()
	k.t = k.t.Add(d)
	k.mu.Unlock()
}

func newStale(stale time.Duration) (*Cached, *flakyStore, *testClock) {
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	next := &flakyStore{active: map[string]bool{"live": true, "gone": false}}
	c := NewCached(next, time.Minute).WithStale(stale)
	c.now = clk.now
	return c, next, clk
}

// Regressão: com stale 0, o erro do banco continua sendo repassado, mesmo com
// entrada conhecida e vencida, e o circuit breaker não age.
func TestStaleOffKeepsErrors(t *testing.T) {
	c, next, clk := newStale(0)
	ctx := context.Background()
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatal("deveria aceitar")
	}
	next.set(true)
	clk.add(2 * time.Minute)
	base := next.n()
	for range 3 {
		if _, err := c.Active(ctx, "live"); err == nil {
			t.Fatal("sem stale, o erro deveria passar")
		}
	}
	if got := next.n() - base; got != 3 {
		t.Fatalf("sem stale cada chamada consulta o banco: %d consultas, quero 3", got)
	}
	if c.StaleServed() != 0 {
		t.Fatal("contador deveria ser zero")
	}
}

func TestStaleServesKnownSiteUntilTTL(t *testing.T) {
	c, next, clk := newStale(10 * time.Minute)
	ctx := context.Background()
	c.Active(ctx, "live")
	next.set(true)

	clk.add(time.Minute) // vencida, dentro do stale
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("deveria servir vencida: %v %v", ok, err)
	}
	clk.add(9*time.Minute + 59*time.Second) // ainda dentro (vencida há < 10 min)
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("ainda dentro do stale: %v %v", ok, err)
	}
	if c.StaleServed() != 2 {
		t.Fatalf("contador = %d, quero 2", c.StaleServed())
	}
	clk.add(2 * time.Second) // passou do stale
	if _, err := c.Active(ctx, "live"); err == nil {
		t.Fatal("depois do stale deveria voltar o erro")
	}
	if c.StaleServed() != 2 {
		t.Fatal("erro não conta como vencida servida")
	}
}

func TestStaleNeverAcceptsUnknownSite(t *testing.T) {
	c, next, _ := newStale(time.Hour)
	next.set(true)
	ok, err := c.Active(context.Background(), "live") // nunca visto
	if ok || err == nil {
		t.Fatalf("site nunca visto: ok=%v err=%v, quero erro", ok, err)
	}
}

// Um site recusado nunca vira aceito por estar vencido, e não ganha stale.
func TestStaleNeverTurnsRefusedIntoAccepted(t *testing.T) {
	c, next, clk := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "gone")
	next.set(true)
	clk.add(5 * time.Minute)
	if ok, _ := c.Active(ctx, "gone"); ok {
		t.Fatal("recusado nunca pode virar aceito")
	}
	// Um site aceito que passa a ser recusado perde a entrada aceita.
	next.set(false)
	next.mu.Lock()
	next.active["live"] = true
	next.mu.Unlock()
	c.Active(ctx, "live")
	next.mu.Lock()
	next.active["live"] = false
	next.mu.Unlock()
	clk.add(2 * time.Minute)
	c.Active(ctx, "live") // volta recusado
	next.set(true)
	clk.add(2 * time.Minute)
	if ok, _ := c.Active(ctx, "live"); ok {
		t.Fatal("site que virou recusado não pode voltar pelo stale")
	}
}

// A revogação durante a falha só vale quando o banco volta.
func TestStaleRevocationAppliesWhenDBReturns(t *testing.T) {
	c, next, clk := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "live")
	next.set(true)
	next.mu.Lock()
	next.active["live"] = false // removido durante a queda
	next.mu.Unlock()
	clk.add(5 * time.Minute)
	if ok, _ := c.Active(ctx, "live"); !ok {
		t.Fatal("durante a queda a resposta vencida ainda vale")
	}
	next.set(false)
	clk.add(errBackoff) // o circuito deixa a sonda passar
	// A sonda é em segundo plano; a primeira chamada ainda recebe o vencido.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, err := c.Active(ctx, "live")
		if !ok && err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("com o banco de volta deveria recusar: %v %v", ok, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitWaiters espera, sem dormir às cegas, até n requisições estarem presas na
// consulta em voo de id.
func waitWaiters(t *testing.T, c *Cached, id string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		cl := c.inflight[id]
		got := 0
		if cl != nil {
			got = cl.waiters
		}
		c.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("esperadores = %d, quero %d", got, n)
		}
		runtime.Gosched()
	}
}

func TestStaleSingleFlight(t *testing.T) {
	c, next, clk := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "live")
	base := next.n()
	next.set(true)
	gate := make(chan struct{})
	next.setGate(gate)
	clk.add(5 * time.Minute)

	const n = 50
	var wg sync.WaitGroup
	results := make(chan bool, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := c.Active(ctx, "live")
			results <- ok && err == nil
		}()
	}
	waitWaiters(t, c, "live", n)
	close(gate)
	wg.Wait()
	close(results)
	for r := range results {
		if !r {
			t.Fatal("todas deveriam receber a resposta vencida")
		}
	}
	if got := next.n() - base; got != 1 {
		t.Fatalf("consultas em voo = %d, quero 1", got)
	}
}

func TestCanceledWaiterDoesNotBreakOthers(t *testing.T) {
	c, next, _ := newStale(0)
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	next.mu.Lock()
	next.gate, next.started = gate, started
	next.mu.Unlock()
	cctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := c.Active(cctx, "live"); errc <- err }()
	<-started
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("quem cancelou deveria receber erro")
	}
	close(gate)
	// A consulta compartilhada continuou: esta chamada entra nela ou lê o cache.
	if ok, err := c.Active(context.Background(), "live"); !ok || err != nil {
		t.Fatalf("a consulta compartilhada deveria ter sido aproveitada: %v %v", ok, err)
	}
}

// IDs inventados só enchem o mapa negativo: o site aceito sobrevive, também
// durante a falha do banco.
func TestStaleSaturationKeepsAcceptedSite(t *testing.T) {
	c, next, clk := newStale(time.Hour)
	ctx := context.Background()
	if ok, _ := c.Active(ctx, "live"); !ok {
		t.Fatal("deveria aceitar")
	}
	// Enche o cache com mais negativas do que o teto.
	for i := range maxCached + 500 {
		_, _ = c.Active(ctx, fmt.Sprintf("fake-%d", i))
	}
	c.mu.Lock()
	if len(c.neg) > maxCached || len(c.entries) != 1 {
		t.Fatalf("neg=%d entries=%d", len(c.neg), len(c.entries))
	}
	c.mu.Unlock()

	// Banco cai; mais IDs inventados, e o site bom segue sendo servido vencido.
	next.set(true)
	clk.add(5 * time.Minute)
	for i := range 3000 {
		if ok, _ := c.Active(ctx, fmt.Sprintf("fake2-%d", i)); ok {
			t.Fatal("inventado não pode ser aceito")
		}
	}
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("site aceito perdido na saturação: %v %v", ok, err)
	}
}

func TestAcceptedMapIsBounded(t *testing.T) {
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	always := storeFunc(func(context.Context, string) (bool, error) { return true, nil })
	c := NewCached(always, time.Hour).WithStale(time.Hour)
	c.now = clk.now
	for i := range maxCached + 50 {
		clk.add(time.Millisecond)
		_, _ = c.Active(context.Background(), fmt.Sprintf("s-%d", i))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > maxCached {
		t.Fatalf("aceitos = %d, limite %d", len(c.entries), maxCached)
	}
	if _, ok := c.entries[fmt.Sprintf("s-%d", maxCached+49)]; !ok {
		t.Fatal("a entrada mais nova deveria estar no cache")
	}
}

type storeFunc func(context.Context, string) (bool, error)

func (f storeFunc) Active(ctx context.Context, id string) (bool, error) { return f(ctx, id) }

// Banco pendurado: a primeira requisição espera o timeout; depois o circuito
// abre e as seguintes recebem o vencido na hora, sem novas consultas, até a
// sonda de depois do backoff.
func TestStaleTimeoutAndCircuitBreaker(t *testing.T) {
	c, next, clk := newStale(time.Hour)
	c.timeout = 50 * time.Millisecond
	ctx := context.Background()
	c.Active(ctx, "live")
	base := next.n()
	next.set(true)
	next.setGate(make(chan struct{})) // nunca abre: o banco pendura
	clk.add(5 * time.Minute)

	start := time.Now()
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("deveria servir o vencido depois do timeout: %v %v", ok, err)
	}
	if d := time.Since(start); d < 40*time.Millisecond || d > 2*time.Second {
		t.Fatalf("esperou %v, esperado perto do timeout de 50ms", d)
	}

	// Circuito aberto: resposta imediata e nenhuma consulta nova.
	start = time.Now()
	for range 100 {
		if ok, err := c.Active(ctx, "live"); !ok || err != nil {
			t.Fatalf("circuito aberto deveria servir o vencido: %v %v", ok, err)
		}
		if _, err := c.Active(ctx, "never-seen"); err == nil {
			t.Fatal("desconhecido deveria receber o erro")
		}
	}
	if d := time.Since(start); d > 40*time.Millisecond {
		t.Fatalf("circuito aberto demorou %v", d)
	}
	if got := next.n() - base; got != 1 {
		t.Fatalf("consultas = %d, quero 1 (o resto veio do circuito)", got)
	}

	// Depois do backoff, uma sonda (só uma) testa o banco, ainda pendurado.
	clk.add(errBackoff)
	for range 20 {
		if ok, _ := c.Active(ctx, "live"); !ok {
			t.Fatal("durante a sonda o vencido continua valendo")
		}
	}
	// A sonda expira em 50ms e reabre o circuito.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		done := !c.probing && c.failUntil.After(clk.now())
		c.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sonda não terminou")
		}
		runtime.Gosched()
	}
	if got := next.n() - base; got != 2 {
		t.Fatalf("consultas = %d, quero 2 (a primeira e uma sonda)", got)
	}

	// O banco volta: a próxima sonda fecha o circuito.
	next.set(false)
	next.setGate(nil)
	clk.add(errBackoff)
	deadline = time.Now().Add(2 * time.Second)
	for {
		c.Active(ctx, "live")
		c.mu.Lock()
		failed := c.failed
		c.mu.Unlock()
		if !failed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("circuito não fechou")
		}
		runtime.Gosched()
	}
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("depois da recuperação: %v %v", ok, err)
	}
}
