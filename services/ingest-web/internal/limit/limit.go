// Package limit aplica um token bucket por chave (o IP já truncado).
package limit

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter guarda um rate.Limiter por chave, só em memória.
type Limiter struct {
	rate  rate.Limit
	burst int
	idle  time.Duration
	now   func() time.Time

	mu      sync.Mutex
	buckets map[netip.Addr]*bucket
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// New cria um Limiter com perSecond eventos por segundo e rajada burst.
// Chaves sem uso há mais de idle são removidas por Run.
func New(perSecond float64, burst int, idle time.Duration) *Limiter {
	return &Limiter{
		rate:    rate.Limit(perSecond),
		burst:   burst,
		idle:    idle,
		now:     time.Now,
		buckets: make(map[netip.Addr]*bucket),
	}
}

// Allow consome um token da chave e diz se o evento pode seguir.
func (l *Limiter) Allow(key netip.Addr) bool {
	now := l.now()
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	l.mu.Unlock()
	return b.lim.AllowN(now, 1)
}

// Sweep remove as chaves paradas há mais de idle.
func (l *Limiter) Sweep() {
	cutoff := l.now().Add(-l.idle)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if b.seen.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// Len diz quantas chaves estão em memória.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Run chama Sweep a cada every até o contexto acabar.
func (l *Limiter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.Sweep()
		}
	}
}
