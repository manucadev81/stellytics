package site

import (
	"context"
	"errors"
	"fmt"
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
	if n := len(c.entries); n > maxCached {
		t.Fatalf("cache com %d entradas, limite %d", n, maxCached)
	}
}

// flakyStore troca entre respostas e falhas e é seguro para uso concorrente.
type flakyStore struct {
	mu     sync.Mutex
	calls  int
	active map[string]bool
	fail   bool
	gate   chan struct{} // se não nulo, Active espera nele
}

func (s *flakyStore) Active(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	s.calls++
	fail, a, gate := s.fail, s.active[id], s.gate
	s.mu.Unlock()
	if gate != nil {
		<-gate
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

func (s *flakyStore) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newStale(stale time.Duration) (*Cached, *flakyStore, *time.Time) {
	now := time.Unix(1_700_000_000, 0)
	next := &flakyStore{active: map[string]bool{"live": true, "gone": false}}
	c := NewCached(next, time.Minute).WithStale(stale)
	c.now = func() time.Time { return now }
	return c, next, &now
}

// Regressão: com stale 0, o erro do banco continua sendo repassado, mesmo com
// entrada conhecida e vencida.
func TestStaleOffKeepsOldBehavior(t *testing.T) {
	c, next, now := newStale(0)
	ctx := context.Background()
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatal("deveria aceitar")
	}
	next.set(true)
	*now = now.Add(2 * time.Minute)
	if _, err := c.Active(ctx, "live"); err == nil {
		t.Fatal("sem stale, o erro deveria passar")
	}
	if c.StaleServed() != 0 {
		t.Fatal("contador deveria ser zero")
	}
}

func TestStaleServesKnownSiteUntilTTL(t *testing.T) {
	c, next, now := newStale(10 * time.Minute)
	ctx := context.Background()
	c.Active(ctx, "live")
	next.set(true)

	*now = now.Add(time.Minute) // vencida, dentro do stale
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("deveria servir vencida: %v %v", ok, err)
	}
	*now = now.Add(9*time.Minute + 59*time.Second) // ainda dentro (vencida há < 10 min)
	if ok, err := c.Active(ctx, "live"); !ok || err != nil {
		t.Fatalf("ainda dentro do stale: %v %v", ok, err)
	}
	if c.StaleServed() != 2 {
		t.Fatalf("contador = %d, quero 2", c.StaleServed())
	}
	*now = now.Add(2 * time.Second) // passou do stale
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

func TestStaleNeverTurnsRefusedIntoAccepted(t *testing.T) {
	c, next, now := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "gone")
	next.set(true)
	*now = now.Add(5 * time.Minute)
	ok, err := c.Active(ctx, "gone")
	if ok {
		t.Fatal("recusado nunca pode virar aceito")
	}
	if err != nil {
		t.Fatalf("recusado conhecido deveria seguir recusado, não erro: %v", err)
	}
}

// A revogação durante a falha só vale quando o banco volta.
func TestStaleRevocationAppliesWhenDBReturns(t *testing.T) {
	c, next, now := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "live")
	next.set(true)
	next.mu.Lock()
	next.active["live"] = false // removido durante a queda
	next.mu.Unlock()
	*now = now.Add(5 * time.Minute)
	if ok, _ := c.Active(ctx, "live"); !ok {
		t.Fatal("durante a queda a resposta vencida ainda vale")
	}
	next.set(false)
	if ok, err := c.Active(ctx, "live"); ok || err != nil {
		t.Fatalf("com o banco de volta deveria recusar: %v %v", ok, err)
	}
}

func TestStaleSingleFlight(t *testing.T) {
	c, next, now := newStale(time.Hour)
	ctx := context.Background()
	c.Active(ctx, "live")
	base := next.n()
	next.set(true)
	next.mu.Lock()
	next.gate = make(chan struct{})
	next.mu.Unlock()
	*now = now.Add(5 * time.Minute)

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
	// Dá tempo de todas entrarem na espera antes de liberar a consulta.
	time.Sleep(100 * time.Millisecond)
	close(next.gate)
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

func TestStaleCanceledWaiterDoesNotBreakOthers(t *testing.T) {
	c, next, _ := newStale(0)
	gate := make(chan struct{})
	next.mu.Lock()
	next.gate = gate
	next.mu.Unlock()
	cctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := c.Active(cctx, "live"); errc <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("quem cancelou deveria receber erro")
	}
	close(gate)
	// A consulta continuou e foi guardada.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok, err := c.Active(context.Background(), "live"); ok && err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a consulta compartilhada deveria ter sido guardada")
}

func TestStaleBoundedAndRace(t *testing.T) {
	c, _, now := newStale(time.Hour)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 2000 {
				_, _ = c.Active(context.Background(), fmt.Sprintf("%d-%d", g, i))
			}
		}()
	}
	wg.Wait()
	_ = now
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > maxCached || len(c.inflight) != 0 {
		t.Fatalf("entradas=%d inflight=%d", len(c.entries), len(c.inflight))
	}
}
