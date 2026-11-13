package limit

import (
	"net/netip"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := New(1, 2, time.Minute)
	l.now = func() time.Time { return now }

	a := netip.MustParseAddr("203.0.113.0")
	b := netip.MustParseAddr("198.51.100.0")

	if !l.Allow(a) || !l.Allow(a) {
		t.Fatal("rajada de 2 recusada")
	}
	if l.Allow(a) {
		t.Fatal("terceiro evento passou do limite")
	}
	if !l.Allow(b) {
		t.Fatal("outra chave não deveria ser afetada")
	}

	now = now.Add(time.Second)
	if !l.Allow(a) {
		t.Fatal("token não foi reposto depois de 1 s")
	}

	now = now.Add(2 * time.Minute)
	l.Sweep()
	if n := l.Len(); n != 0 {
		t.Fatalf("Sweep deixou %d chaves", n)
	}
}
