// Package fingerprint desconfia de muitos navegadores diferentes no mesmo IP (#26).
//
// A fingerprint é um hash de User-Agent, Accept-Language, idioma e tela. Ela não
// identifica uma pessoa (muita gente tem a mesma combinação) e só existe em
// memória, por uma janela curta. Por IP truncado, o Counter aceita até Limit
// fingerprints distintas vistas dentro da janela; eventos de fingerprints além
// disso são descartados. Um robô que troca de User-Agent a cada request, a
// partir de um IP só, não infla as métricas; as primeiras fingerprints do IP
// continuam contando normalmente.
package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"net/netip"
	"sync"
	"time"
)

// Of calcula a fingerprint de um evento. Os campos são separados por um byte
// zero para que "ab"+"c" e "a"+"bc" deem hashes diferentes.
func Of(userAgent, acceptLanguage, language, screen string) uint64 {
	h := sha256.New()
	for _, s := range []string{userAgent, acceptLanguage, language, screen} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return binary.BigEndian.Uint64(h.Sum(nil)[:8])
}

// Counter conta fingerprints distintas por IP truncado numa janela deslizante.
type Counter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu  sync.Mutex
	ips map[netip.Addr]map[uint64]time.Time
}

// New cria um Counter que aceita até limit fingerprints por IP em window.
// limit 0 desliga a checagem.
func New(limit int, window time.Duration) *Counter {
	return &Counter{limit: limit, window: window, now: time.Now, ips: make(map[netip.Addr]map[uint64]time.Time)}
}

// WithClock troca o relógio (para testes) e devolve o próprio Counter.
func (c *Counter) WithClock(now func() time.Time) *Counter {
	c.now = now
	return c
}

// Allow registra fp no IP truncado ip e diz se o evento pode seguir. Uma
// fingerprint já vista na janela sempre segue; uma nova só entra enquanto o IP
// tiver menos de limit fingerprints ativas.
func (c *Counter) Allow(ip netip.Addr, fp uint64) bool {
	if c.limit <= 0 {
		return true
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := c.ips[ip]
	if seen == nil {
		seen = make(map[uint64]time.Time)
		c.ips[ip] = seen
	}
	if _, ok := seen[fp]; ok {
		seen[fp] = now
		return true
	}
	if len(seen) >= c.limit {
		c.expire(seen, now)
		if len(seen) >= c.limit {
			return false
		}
	}
	seen[fp] = now
	return true
}

func (c *Counter) expire(seen map[uint64]time.Time, now time.Time) {
	cutoff := now.Add(-c.window)
	for fp, at := range seen {
		if at.Before(cutoff) {
			delete(seen, fp)
		}
	}
}

// Sweep tira da memória as fingerprints fora da janela e os IPs sem nenhuma.
func (c *Counter) Sweep() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for ip, seen := range c.ips {
		c.expire(seen, now)
		if len(seen) == 0 {
			delete(c.ips, ip)
		}
	}
}

// Len diz quantos IPs estão em memória.
func (c *Counter) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ips)
}

// Run chama Sweep a cada every até o contexto acabar.
func (c *Counter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Sweep()
		}
	}
}
