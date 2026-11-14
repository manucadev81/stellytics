package main

// Teste da #26: muitas fingerprints no mesmo IP truncado não inflam as métricas.

import (
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/romulospl/notify/services/ingest-web/internal/clientip"
	"github.com/romulospl/notify/services/ingest-web/internal/fingerprint"
)

// otherActiveSite é o site de outro tenant, também ativo.
const otherActiveSite = "11111111-2222-4333-8444-555555555555"

func TestFingerprintLimitPerIP(t *testing.T) {
	s := newSetup(t, func(g *gateway) {
		g.fps = fingerprint.New(2, 10*time.Minute)
		g.sites = fakeSites{activeSite: true, otherActiveSite: true}
		g.ips = clientip.Resolver{Trusted: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	})
	send := func(ip, ua string) {
		t.Helper()
		resp := post(t, s, "text/plain", eventBody(activeSite), "X-Forwarded-For", ip, "User-Agent", ua)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, quero 204 mesmo quando descarta", resp.StatusCode)
		}
	}

	// Um robô troca de User-Agent a cada request no mesmo /24.
	send("203.0.113.10", "bot/1")
	send("203.0.113.11", "bot/2")
	send("203.0.113.12", "bot/3")
	send("203.0.113.13", "bot/4")
	// As duas primeiras fingerprints continuam valendo; outro IP tem a própria conta.
	send("203.0.113.14", "bot/1")
	send("198.51.100.7", "bot/3")
	// Outro site no mesmo /24 também: o robô não lota a cota dos outros tenants.
	resp := post(t, s, "text/plain", eventBody(otherActiveSite), "X-Forwarded-For", "203.0.113.15", "User-Agent", "humano")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var uas []string
	for _, r := range s.umami.received() {
		uas = append(uas, r.header.Get("User-Agent")+" @ "+r.header.Get("x-notify-client-ip"))
	}
	want := []string{
		"bot/1 @ 203.0.113.0",
		"bot/2 @ 203.0.113.0",
		"bot/1 @ 203.0.113.0",
		"bot/3 @ 198.51.100.0",
		"humano @ 203.0.113.0",
	}
	if len(uas) != len(want) {
		t.Fatalf("umami recebeu %v, quero %v", uas, want)
	}
	for i := range want {
		if uas[i] != want[i] {
			t.Errorf("evento %d = %q, quero %q", i, uas[i], want[i])
		}
	}
}
