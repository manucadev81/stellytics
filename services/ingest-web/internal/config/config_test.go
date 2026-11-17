package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{
		"DATABASE_URL": "postgresql://stellytics_ingest:x@db/stellytics",
		"UMAMI_URL":    "http://umami:3000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.ClientIPHeader != "x-stellytics-client-ip" ||
		c.RateLimit != 20 || c.RateBurst != 40 || c.SiteCacheTTL != time.Minute || c.SiteStaleTTL != 0 ||
		c.ScriptDir != "" || len(c.TrustedProxies) != 0 || c.UmamiURL.Host != "umami:3000" ||
		c.ProofSecret != nil || c.ProofBits != 12 || c.ProofTTL != 10*time.Minute ||
		c.FingerprintLimit != 50 || c.FingerprintWindow != 10*time.Minute ||
		c.RelayMaxInflight != 0 || c.RelayQueueWait != 250*time.Millisecond {
		t.Errorf("padrões inesperados: %+v", c)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	c, err := Load(env(map[string]string{
		"DATABASE_URL":    "x",
		"UMAMI_URL":       "http://umami:3000",
		"TRUSTED_PROXIES": " 172.16.0.0/12, 10.0.0.5 ,fd00::/8, 192.168.1.9/24",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"172.16.0.0/12", "10.0.0.5/32", "fd00::/8", "192.168.1.0/24"}
	if len(c.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v", c.TrustedProxies)
	}
	for i, p := range c.TrustedProxies {
		if p.String() != want[i] {
			t.Errorf("TrustedProxies[%d] = %s, quero %s", i, p, want[i])
		}
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"sem DATABASE_URL": {"UMAMI_URL": "http://umami:3000"},
		"sem UMAMI_URL":    {"DATABASE_URL": "x"},
		"UMAMI_URL ruim":   {"DATABASE_URL": "x", "UMAMI_URL": "umami"},
		"proxy ruim":       {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "TRUSTED_PROXIES": "lixo"},
		"taxa ruim":        {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RATE_LIMIT": "0"},
		"rajada ruim":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RATE_BURST": "-1"},
		"ttl ruim":         {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_CACHE_TTL": "1 minuto"},
		"ttl longo":        {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_CACHE_TTL": "2m"},
		"stale ruim":       {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_STALE_TTL": "1 hora"},
		"stale longo":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_STALE_TTL": "2h"},
		"stale negativo":   {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_STALE_TTL": "-1s"},
		"segredo curto":    {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "PROOF_SECRET": "curto"},
		"bits demais":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "PROOF_BITS": "25"},
		"prova longa":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "PROOF_TTL": "2h"},
		"limite negativo":  {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "FINGERPRINT_LIMIT": "-1"},
		"limite alto":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "FINGERPRINT_LIMIT": "1001"},
		"janela curta":     {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "FINGERPRINT_WINDOW": "30s"},
		"relay negativo":   {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_MAX_INFLIGHT": "-1"},
		"relay alto":       {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_MAX_INFLIGHT": "100001"},
		"relay lixo":       {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_MAX_INFLIGHT": "muitos"},
		"espera negativa":  {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_QUEUE_WAIT": "-1s"},
		"espera longa":     {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_QUEUE_WAIT": "11s"},
		"espera lixo":      {"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_QUEUE_WAIT": "rapido"},
	}
	for name, m := range cases {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: sem erro", name)
		}
	}
}

func TestLoadSiteStaleTTL(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "x", "UMAMI_URL": "http://u", "SITE_STALE_TTL": "1h"}
	c, err := Load(func(k string) string { return env[k] })
	if err != nil || c.SiteStaleTTL != time.Hour {
		t.Fatalf("SITE_STALE_TTL=1h: %v %v", c.SiteStaleTTL, err)
	}
}

func TestLoadRelayLimit(t *testing.T) {
	m := map[string]string{"DATABASE_URL": "x", "UMAMI_URL": "http://u", "RELAY_MAX_INFLIGHT": "100000", "RELAY_QUEUE_WAIT": "0"}
	c, err := Load(func(k string) string { return m[k] })
	if err != nil || c.RelayMaxInflight != 100000 || c.RelayQueueWait != 0 {
		t.Fatalf("limites: %+v %v", c, err)
	}
}
