package fingerprint

import (
	"net/netip"
	"sync"
	"testing"
	"time"
)

var (
	ip        = netip.MustParseAddr("203.0.113.0")
	otherIP   = netip.MustParseAddr("198.51.100.0")
	site      = "6f1c2b9e-3a4d-4c8e-9f00-1234567890ab"
	otherSite = "0a0b0c0d-0e0f-4000-8000-000000000001"
)

func TestOf(t *testing.T) {
	base := Of("Mozilla/5.0 Firefox", "pt-BR", "pt-BR", "1920x1080")
	if base != Of("Mozilla/5.0 Firefox", "pt-BR", "pt-BR", "1920x1080") {
		t.Error("mesma entrada, fingerprints diferentes")
	}
	for name, fp := range map[string]uint64{
		"outro UA":                      Of("Mozilla/5.0 Chrome", "pt-BR", "pt-BR", "1920x1080"),
		"outro idioma":                  Of("Mozilla/5.0 Firefox", "en-US", "pt-BR", "1920x1080"),
		"outra tela":                    Of("Mozilla/5.0 Firefox", "pt-BR", "pt-BR", "390x844"),
		"campos colados de outro jeito": Of("Mozilla/5.0 Firefoxp", "t-BR", "pt-BR", "1920x1080"),
	} {
		if fp == base {
			t.Errorf("%s: mesma fingerprint", name)
		}
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAllow(t *testing.T) {
	clk := &clock{t: time.Unix(1_800_000_000, 0)}
	c := New(3, 10*time.Minute).WithClock(clk.now)

	for fp := uint64(1); fp <= 3; fp++ {
		if !c.Allow(ip, site, fp) {
			t.Fatalf("fingerprint %d dentro do limite recusada", fp)
		}
	}
	if c.Allow(ip, site, 4) {
		t.Error("quarta fingerprint no mesmo IP aceita")
	}
	// As já vistas continuam passando, e outro IP ou outro site têm a própria
	// conta: lotar a cota de um site não tira as vagas dos outros.
	if !c.Allow(ip, site, 2) {
		t.Error("fingerprint conhecida recusada")
	}
	if !c.Allow(otherIP, site, 4) {
		t.Error("outro IP recusado")
	}
	for fp := uint64(1); fp <= 3; fp++ {
		if !c.Allow(ip, otherSite, 100+fp) {
			t.Errorf("outro site no mesmo IP recusado (fingerprint %d)", 100+fp)
		}
	}

	// Passada a janela sem eventos das antigas, cabem fingerprints novas.
	clk.t = clk.t.Add(5 * time.Minute)
	c.Allow(ip, site, 3) // a 3 continua ativa
	clk.t = clk.t.Add(6 * time.Minute)
	if !c.Allow(ip, site, 5) {
		t.Error("fingerprint nova recusada depois que as antigas saíram da janela")
	}
	if !c.Allow(ip, site, 6) {
		t.Error("segunda vaga liberada recusada")
	}
	if c.Allow(ip, site, 7) {
		t.Error("limite ignorado: 3, 5 e 6 ainda estão na janela")
	}
}

func TestDisabled(t *testing.T) {
	c := New(0, time.Minute)
	for fp := uint64(0); fp < 100; fp++ {
		if !c.Allow(ip, site, fp) {
			t.Fatal("limite 0 deveria desligar a checagem")
		}
	}
	if c.Len() != 0 {
		t.Error("checagem desligada guardou IP")
	}
}

func TestSweep(t *testing.T) {
	clk := &clock{t: time.Unix(1_800_000_000, 0)}
	c := New(3, 10*time.Minute).WithClock(clk.now)
	c.Allow(ip, site, 1)
	c.Allow(otherIP, site, 1)
	clk.t = clk.t.Add(5 * time.Minute)
	c.Allow(otherIP, site, 2)
	clk.t = clk.t.Add(6 * time.Minute)
	c.Sweep()
	if c.Len() != 1 {
		t.Errorf("Len = %d, quero 1 (só o IP com fingerprint recente)", c.Len())
	}
}

func TestFullAccountKeepsNoNewState(t *testing.T) {
	c := New(2, 10*time.Minute)
	c.Allow(ip, site, 1)
	c.Allow(ip, site, 2)
	for fp := uint64(3); fp < 100; fp++ {
		if c.Allow(ip, site, fp) {
			t.Fatalf("fingerprint %d aceita com a conta cheia", fp)
		}
	}
	c.mu.Lock()
	n := len(c.keys[key{ip, site}])
	c.mu.Unlock()
	if n != 2 {
		t.Errorf("conta cheia guardou %d fingerprints, quero 2", n)
	}
}

// Rode com -race: Allow e Sweep ao mesmo tempo.
func TestConcurrentAllowAndSweep(t *testing.T) {
	c := New(10, time.Minute)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				c.Allow(netip.AddrFrom4([4]byte{10, 0, byte(g), 0}), site, uint64(i%20))
				if i%50 == 0 {
					c.Sweep()
				}
			}
		}(g)
	}
	wg.Wait()
}
