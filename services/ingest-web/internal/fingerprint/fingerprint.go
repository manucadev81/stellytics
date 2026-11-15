// Package fingerprint desconfia de muitos navegadores diferentes no mesmo IP (#26).
//
// A fingerprint é um hash de User-Agent, Accept-Language, idioma e tela. Ela não
// identifica uma pessoa (muita gente tem a mesma combinação) e só existe em
// memória, por uma janela curta. Por IP truncado e site, o Counter aceita até
// Limit fingerprints distintas vistas dentro da janela; eventos de
// fingerprints além disso são descartados. Um robô que troca de User-Agent a
// cada request, a partir de um IP só, não infla as métricas; as primeiras
// fingerprints do IP continuam contando normalmente.
//
// A conta é separada por site: um robô no mesmo /24 (ou na mesma CGNAT) que
// lote a cota do próprio site não tira as vagas dos sites de outros tenants.
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

	mu   sync.Mutex
	keys map[key]map[uint64]time.Time
}

// key é a conta: o IP truncado e o site (UUID em minúsculas).
type key struct {
	ip   netip.Addr
	site string
}

// New cria um Counter que aceita até limit fingerprints por IP em window.
// limit 0 desliga a checagem.
func New(limit int, window time.Duration) *Counter {
	return &Counter{limit: limit, window: window, now: time.Now, keys: make(map[key]map[uint64]time.Time)}
}

// WithClock troca o relógio (para testes) e devolve o próprio Counter.
func (c *Counter) WithClock(now func() time.Time) *Counter {
	c.now = now
	return c
}

// Allow registra fp na conta do IP truncado ip no site e diz se o evento pode
// seguir. Uma fingerprint já vista na janela sempre segue; uma nova só entra
// enquanto a conta tiver menos de limit fingerprints ativas.
func (c *Counter) Allow(ip netip.Addr, site string, fp uint64) bool {
	ok, _ := c.Admit(ip, site, fp)
	return ok
}

// Admit é o Allow que também diz se a fingerprint entrou agora na conta
// (fresh). Quem descarta o evento depois por outro motivo (saturação do
// repasse) chama Forget com fresh verdadeiro, para o descarte não gastar uma
// das vagas de fingerprint do IP.
func (c *Counter) Admit(ip netip.Addr, site string, fp uint64) (ok, fresh bool) {
	if c.limit <= 0 {
		return true, false
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	k := key{ip, site}
	seen := c.keys[k]
	if seen == nil {
		seen = make(map[uint64]time.Time)
		c.keys[k] = seen
	}
	if _, ok := seen[fp]; ok {
		seen[fp] = now
		return true, false
	}
	if len(seen) >= c.limit {
		c.expire(seen, now)
		if len(seen) >= c.limit {
			return false, false
		}
	}
	seen[fp] = now
	return true, true
}

// Forget tira fp da conta do IP truncado ip no site. Só para desfazer um
// Admit que devolveu fresh.
func (c *Counter) Forget(ip netip.Addr, site string, fp uint64) {
	if c.limit <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key{ip, site}
	if seen := c.keys[k]; seen != nil {
		delete(seen, fp)
		if len(seen) == 0 {
			delete(c.keys, k)
		}
	}
}

func (c *Counter) expire(seen map[uint64]time.Time, now time.Time) {
	cutoff := now.Add(-c.window)
	for fp, at := range seen {
		if at.Before(cutoff) {
			delete(seen, fp)
		}
	}
}

// Sweep tira da memória as fingerprints fora da janela e as contas sem nenhuma.
func (c *Counter) Sweep() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, seen := range c.keys {
		c.expire(seen, now)
		if len(seen) == 0 {
			delete(c.keys, k)
		}
	}
}

// Len diz quantas contas (IP e site) estão em memória.
func (c *Counter) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.keys)
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
