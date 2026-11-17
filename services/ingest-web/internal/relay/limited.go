package relay

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSaturated indica que não surgiu vaga de repasse dentro de QueueWait: o
// evento é descartado, não adiado.
var ErrSaturated = errors.New("relay: repasses simultâneos no limite")

// Sender é o que o gateway usa para repassar um evento.
type Sender interface {
	Send(ctx context.Context, ev Event, userAgent string, truncatedIP netip.Addr) error
}

// Limited limita quantos repasses ao Umami correm ao mesmo tempo (semáforo).
// Quem chega com todas as vagas ocupadas espera até wait por uma; passado o
// prazo, Send devolve ErrSaturated sem tocar no Umami. Assim o gateway nunca
// segura mais que max requisições no Umami nem mais que (chegadas por segundo
// x wait) esperando vaga, em vez de acumular tudo enquanto o Umami está lento.
//
// A vaga é devolvida por defer: erro, timeout, cancelamento do cliente e panic
// do Sender a liberam.
type Limited struct {
	next    Sender
	slots   chan struct{}
	wait    time.Duration
	dropped atomic.Uint64
	mu      sync.Mutex
	bySite  map[string]int
	changed chan struct{}
}

// NewLimited envolve next com até max repasses simultâneos e espera de até
// wait por uma vaga (wait 0 recusa na hora). max precisa ser maior que zero.
func NewLimited(next Sender, max int, wait time.Duration) *Limited {
	return &Limited{next: next, slots: make(chan struct{}, max), wait: wait, bySite: make(map[string]int), changed: make(chan struct{})}
}

// Send repassa ev quando há (ou surge a tempo) uma vaga.
func (l *Limited) Send(ctx context.Context, ev Event, userAgent string, truncatedIP netip.Addr) error {
	if err := l.acquire(ctx, ev.Payload.Website); err != nil {
		return err
	}
	defer l.release(ev.Payload.Website)
	return l.next.Send(ctx, ev, userAgent, truncatedIP)
}

// Uma vaga permanece reservada para outros sites quando há pelo menos duas.
// O mapa só contém sites com repasses ativos e tem no máximo cap(slots) entradas.
func (l *Limited) acquire(ctx context.Context, site string) error {
	var timer *time.Timer
	if l.wait > 0 {
		timer = time.NewTimer(l.wait)
		defer timer.Stop()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		limit := cap(l.slots)
		if limit > 1 {
			limit--
		}
		if len(l.slots) < cap(l.slots) && l.bySite[site] < limit {
			l.slots <- struct{}{}
			l.bySite[site]++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		if timer == nil {
			l.dropped.Add(1)
			return ErrSaturated
		}
		select {
		case <-changed:
		case <-timer.C:
			l.dropped.Add(1)
			return ErrSaturated
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *Limited) release(site string) {
	l.mu.Lock()
	<-l.slots
	l.bySite[site]--
	if l.bySite[site] == 0 {
		delete(l.bySite, site)
	}
	close(l.changed)
	l.changed = make(chan struct{})
	l.mu.Unlock()
}

// Dropped é o total de eventos recusados por saturação desde o início.
func (l *Limited) Dropped() uint64 { return l.dropped.Load() }

// InFlight é quantos repasses estão em andamento agora.
func (l *Limited) InFlight() int { return len(l.slots) }
