package relay

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gate é um Sender que segura cada chamada até receber um sinal.
type gate struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
	fn      func()
}

func newGate() *gate {
	return &gate{started: make(chan struct{}, 1000), release: make(chan struct{})}
}

func (g *gate) Send(ctx context.Context, _ Event, _ string, _ netip.Addr) error {
	g.calls.Add(1)
	g.started <- struct{}{}
	if g.fn != nil {
		g.fn()
	}
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func send(l *Limited, ctx context.Context) error {
	return l.Send(ctx, Event{}, "ua", netip.Addr{})
}

func waitStarted(t *testing.T, g *gate, n int) {
	t.Helper()
	for range n {
		select {
		case <-g.started:
		case <-time.After(2 * time.Second):
			t.Fatal("repasse não começou")
		}
	}
}

func TestLimitedCapsInflightAndRefusesAfterWait(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 3, 30*time.Millisecond)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = send(l, context.Background()) }()
	}
	waitStarted(t, g, 2)
	if l.InFlight() != 2 {
		t.Fatalf("em voo = %d", l.InFlight())
	}
	start := time.Now()
	err := send(l, context.Background())
	if !errors.Is(err, ErrSaturated) {
		t.Fatalf("err = %v, quero ErrSaturated", err)
	}
	if d := time.Since(start); d < 25*time.Millisecond || d > time.Second {
		t.Errorf("esperou %v, quero cerca de 30ms", d)
	}
	if g.calls.Load() != 2 || l.Dropped() != 1 {
		t.Errorf("chamadas = %d, descartados = %d", g.calls.Load(), l.Dropped())
	}
	close(g.release)
	wg.Wait()
	if l.InFlight() != 0 {
		t.Errorf("vagas não devolvidas: %d", l.InFlight())
	}
}

func TestLimitedWaiterGetsFreedSlot(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 1, 2*time.Second)
	first := make(chan error, 1)
	go func() { first <- send(l, context.Background()) }()
	waitStarted(t, g, 1)
	second := make(chan error, 1)
	go func() { second <- send(l, context.Background()) }()
	time.Sleep(20 * time.Millisecond) // o segundo está esperando a vaga
	g.release <- struct{}{}           // libera o primeiro
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	waitStarted(t, g, 1) // o segundo pegou a vaga
	g.release <- struct{}{}
	if err := <-second; err != nil {
		t.Fatalf("o que esperou deveria passar: %v", err)
	}
	if l.Dropped() != 0 {
		t.Errorf("descartados = %d", l.Dropped())
	}
}

func TestLimitedZeroWaitRefusesImmediately(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 1, 0)
	go func() { _ = send(l, context.Background()) }()
	waitStarted(t, g, 1)
	if err := send(l, context.Background()); !errors.Is(err, ErrSaturated) {
		t.Fatalf("err = %v", err)
	}
	close(g.release)
}

func TestLimitedReleasesOnPanic(t *testing.T) {
	g := newGate()
	g.fn = func() { panic("boom") }
	l := NewLimited(g, 1, 10*time.Millisecond)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("o panic deveria passar")
			}
		}()
		_ = send(l, context.Background())
	}()
	if l.InFlight() != 0 {
		t.Fatalf("vaga presa depois do panic: %d", l.InFlight())
	}
	g.fn = nil
	close(g.release)
	if err := send(l, context.Background()); err != nil {
		t.Fatalf("depois do panic: %v", err)
	}
}

func TestLimitedReleasesOnErrorAndCancel(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 1, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- send(l, ctx) }()
	waitStarted(t, g, 1)
	cancel() // o cliente desistiu no meio do repasse
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if l.InFlight() != 0 {
		t.Fatalf("vaga presa depois do cancelamento: %d", l.InFlight())
	}
}

func TestLimitedCancelWhileWaitingIsNotCountedAsDrop(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 1, 5*time.Second)
	go func() { _ = send(l, context.Background()) }()
	waitStarted(t, g, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- send(l, ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if l.Dropped() != 0 || g.calls.Load() != 1 {
		t.Errorf("descartados = %d, chamadas = %d", l.Dropped(), g.calls.Load())
	}
	close(g.release)
}

// Muitas chegadas ao mesmo tempo: nunca passa de max em voo, tudo é aceito ou
// recusado, nada fica pendurado e nenhuma goroutine vaza.
func TestLimitedConcurrentNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	var cur, peak, ok, refused atomic.Int64
	inner := senderFunc(func(ctx context.Context) error {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		cur.Add(-1)
		return nil
	})
	l := NewLimited(inner, 4, 3*time.Millisecond)
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := send(l, context.Background()); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrSaturated):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 4 {
		t.Errorf("pico em voo = %d, limite 4", peak.Load())
	}
	if ok.Load()+refused.Load() != 200 || refused.Load() != int64(l.Dropped()) || refused.Load() == 0 || ok.Load() == 0 {
		t.Errorf("aceitos %d, recusados %d, contador %d", ok.Load(), refused.Load(), l.Dropped())
	}
	if l.InFlight() != 0 {
		t.Errorf("em voo no fim = %d", l.InFlight())
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines: %d antes, %d depois", before, n)
	}
}

type senderFunc func(ctx context.Context) error

func (f senderFunc) Send(ctx context.Context, _ Event, _ string, _ netip.Addr) error { return f(ctx) }

func TestLimitedReservesSlotForAnotherSite(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 3, 0)
	event := func(site string) Event { return Event{Payload: Payload{Website: site}} }
	for range 2 {
		go func() { _ = l.Send(context.Background(), event("a"), "", netip.Addr{}) }()
	}
	waitStarted(t, g, 2)
	if err := l.Send(context.Background(), event("a"), "", netip.Addr{}); !errors.Is(err, ErrSaturated) {
		t.Fatalf("site em pico: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- l.Send(context.Background(), event("b"), "", netip.Addr{}) }()
	waitStarted(t, g, 1)
	if l.InFlight() != 3 {
		t.Fatalf("em voo: %d", l.InFlight())
	}
	close(g.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLimitedDoesNotRetainRejectedSites(t *testing.T) {
	g := newGate()
	l := NewLimited(g, 2, 0)
	for _, site := range []string{"a", "b"} {
		go func() { _ = l.Send(context.Background(), Event{Payload: Payload{Website: site}}, "", netip.Addr{}) }()
	}
	waitStarted(t, g, 2)
	for i := 0; i < 10000; i++ {
		err := l.Send(context.Background(), Event{Payload: Payload{Website: strconv.Itoa(i)}}, "", netip.Addr{})
		if !errors.Is(err, ErrSaturated) {
			t.Fatalf("site %d: %v", i, err)
		}
	}
	l.mu.Lock()
	n := len(l.bySite)
	l.mu.Unlock()
	if n != 2 {
		t.Fatalf("sites em memória = %d", n)
	}
	close(g.release)
}
