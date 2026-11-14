package fingerprint

import (
	"net/netip"
	"testing"
	"time"
)

var (
	ip      = netip.MustParseAddr("203.0.113.0")
	otherIP = netip.MustParseAddr("198.51.100.0")
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
		if !c.Allow(ip, fp) {
			t.Fatalf("fingerprint %d dentro do limite recusada", fp)
		}
	}
	if c.Allow(ip, 4) {
		t.Error("quarta fingerprint no mesmo IP aceita")
	}
	// As já vistas continuam passando, e outro IP tem a própria conta.
	if !c.Allow(ip, 2) {
		t.Error("fingerprint conhecida recusada")
	}
	if !c.Allow(otherIP, 4) {
		t.Error("outro IP recusado")
	}

	// Passada a janela sem eventos das antigas, cabem fingerprints novas.
	clk.t = clk.t.Add(5 * time.Minute)
	c.Allow(ip, 3) // a 3 continua ativa
	clk.t = clk.t.Add(6 * time.Minute)
	if !c.Allow(ip, 5) {
		t.Error("fingerprint nova recusada depois que as antigas saíram da janela")
	}
	if !c.Allow(ip, 6) {
		t.Error("segunda vaga liberada recusada")
	}
	if c.Allow(ip, 7) {
		t.Error("limite ignorado: 3, 5 e 6 ainda estão na janela")
	}
}

func TestDisabled(t *testing.T) {
	c := New(0, time.Minute)
	for fp := uint64(0); fp < 100; fp++ {
		if !c.Allow(ip, fp) {
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
	c.Allow(ip, 1)
	c.Allow(otherIP, 1)
	clk.t = clk.t.Add(5 * time.Minute)
	c.Allow(otherIP, 2)
	clk.t = clk.t.Add(6 * time.Minute)
	c.Sweep()
	if c.Len() != 1 {
		t.Errorf("Len = %d, quero 1 (só o IP com fingerprint recente)", c.Len())
	}
}
