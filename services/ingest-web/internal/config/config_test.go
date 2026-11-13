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
		"DATABASE_URL": "postgresql://notify_ingest:x@db/notify",
		"UMAMI_URL":    "http://umami:3000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.ClientIPHeader != "x-notify-client-ip" ||
		c.RateLimit != 20 || c.RateBurst != 40 || c.SiteCacheTTL != time.Minute ||
		c.ScriptDir != "" || len(c.TrustedProxies) != 0 || c.UmamiURL.Host != "umami:3000" {
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
	}
	for name, m := range cases {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: sem erro", name)
		}
	}
}
