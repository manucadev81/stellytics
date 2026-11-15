package relay

import (
	"context"
	"errors"
	"net/netip"
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
}

// NewLimited envolve next com até max repasses simultâneos e espera de até
// wait por uma vaga (wait 0 recusa na hora). max precisa ser maior que zero.
func NewLimited(next Sender, max int, wait time.Duration) *Limited {
	return &Limited{next: next, slots: make(chan struct{}, max), wait: wait}
}

// Send repassa ev quando há (ou surge a tempo) uma vaga.
func (l *Limited) Send(ctx context.Context, ev Event, userAgent string, truncatedIP netip.Addr) error {
	if err := l.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-l.slots }()
	return l.next.Send(ctx, ev, userAgent, truncatedIP)
}

func (l *Limited) acquire(ctx context.Context) error {
	select {
	case l.slots <- struct{}{}:
		return nil
	default:
	}
	if l.wait <= 0 {
		l.dropped.Add(1)
		return ErrSaturated
	}
	t := time.NewTimer(l.wait)
	defer t.Stop()
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-t.C:
		l.dropped.Add(1)
		return ErrSaturated
	case <-ctx.Done():
		// O cliente desistiu: não é saturação, não entra no contador.
		return ctx.Err()
	}
}

// Dropped é o total de eventos recusados por saturação desde o início.
func (l *Limited) Dropped() uint64 { return l.dropped.Load() }

// InFlight é quantos repasses estão em andamento agora.
func (l *Limited) InFlight() int { return len(l.slots) }
