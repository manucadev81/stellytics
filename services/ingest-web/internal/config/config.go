// Package config lê a configuração do gateway a partir de variáveis de ambiente.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config reúne tudo o que o gateway precisa para subir.
type Config struct {
	// ListenAddr é o endereço HTTP do gateway (ex.: ":8080").
	ListenAddr string
	// DatabaseURL aponta para o banco notify com o usuário notify_ingest.
	DatabaseURL string
	// UmamiURL é a base do Umami na rede interna (ex.: "http://umami:3000").
	UmamiURL *url.URL
	// ClientIPHeader é o header em que o IP truncado chega ao Umami.
	ClientIPHeader string
	// ScriptDir é a pasta servida em /s/. Vazia desliga a rota.
	ScriptDir string
	// TrustedProxies são as redes de onde o X-Forwarded-For é aceito.
	TrustedProxies []netip.Prefix
	// RateLimit e RateBurst definem o token bucket por IP truncado.
	RateLimit float64
	RateBurst int
	// SiteCacheTTL é quanto tempo uma resposta da ingest_site fica em memória.
	// No máximo 1 minuto (spec do E2, seção 6).
	SiteCacheTTL time.Duration
	// SiteStaleTTL é por quanto tempo, depois de vencida, a última resposta
	// conhecida de um site ainda vale quando a consulta ao banco falha. Zero
	// (o padrão) desliga. No máximo 1 hora.
	SiteStaleTTL time.Duration
	// ProofSecret assina os desafios da prova de execução de JS (#25). Vazio,
	// o gateway sorteia uma chave ao subir, e os tokens emitidos antes de um
	// reinício (ou por outra réplica) deixam de valer.
	ProofSecret []byte
	// ProofBits é a dificuldade da prova: bits zero no começo do SHA-256.
	ProofBits int
	// ProofTTL é a validade de um desafio.
	ProofTTL time.Duration
	// FingerprintLimit é o máximo de fingerprints distintas por IP truncado e
	// site dentro de FingerprintWindow (#26). Zero desliga a checagem. O teto
	// de 1000 limita a varredura feita sob o lock quando uma conta está cheia.
	FingerprintLimit  int
	FingerprintWindow time.Duration
}

// Load monta a Config a partir de getenv (normalmente os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		ListenAddr:     or(getenv("LISTEN_ADDR"), ":8080"),
		DatabaseURL:    getenv("DATABASE_URL"),
		ClientIPHeader: or(getenv("CLIENT_IP_HEADER"), "x-notify-client-ip"),
		ScriptDir:      getenv("SCRIPT_DIR"),
	}
	var errs []error

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL é obrigatória"))
	}

	raw := getenv("UMAMI_URL")
	if raw == "" {
		errs = append(errs, errors.New("UMAMI_URL é obrigatória"))
	} else if u, err := url.Parse(raw); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("UMAMI_URL inválida: %q", raw))
	} else {
		c.UmamiURL = u
	}

	for _, s := range strings.Split(getenv("TRUSTED_PROXIES"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := parsePrefix(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %w", err))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}

	var err error
	if c.RateLimit, err = parseFloat(getenv("RATE_LIMIT"), 20); err != nil || c.RateLimit <= 0 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT inválido: %q", getenv("RATE_LIMIT")))
	}
	if c.RateBurst, err = parseInt(getenv("RATE_BURST"), 40); err != nil || c.RateBurst <= 0 {
		errs = append(errs, fmt.Errorf("RATE_BURST inválido: %q", getenv("RATE_BURST")))
	}
	if c.SiteCacheTTL, err = parseDuration(getenv("SITE_CACHE_TTL"), time.Minute); err != nil || c.SiteCacheTTL < 0 || c.SiteCacheTTL > time.Minute {
		errs = append(errs, fmt.Errorf("SITE_CACHE_TTL inválido: %q", getenv("SITE_CACHE_TTL")))
	}
	if c.SiteStaleTTL, err = parseDuration(getenv("SITE_STALE_TTL"), 0); err != nil || c.SiteStaleTTL < 0 || c.SiteStaleTTL > time.Hour {
		errs = append(errs, fmt.Errorf("SITE_STALE_TTL inválido (0 a 1h): %q", getenv("SITE_STALE_TTL")))
	}

	if s := getenv("PROOF_SECRET"); s != "" {
		if len(s) < 32 {
			errs = append(errs, errors.New("PROOF_SECRET precisa de pelo menos 32 caracteres"))
		}
		c.ProofSecret = []byte(s)
	}
	if c.ProofBits, err = parseInt(getenv("PROOF_BITS"), 12); err != nil || c.ProofBits < 0 || c.ProofBits > 24 {
		errs = append(errs, fmt.Errorf("PROOF_BITS inválido (0 a 24): %q", getenv("PROOF_BITS")))
	}
	if c.ProofTTL, err = parseDuration(getenv("PROOF_TTL"), 10*time.Minute); err != nil || c.ProofTTL < time.Minute || c.ProofTTL > time.Hour {
		errs = append(errs, fmt.Errorf("PROOF_TTL inválido (1m a 1h): %q", getenv("PROOF_TTL")))
	}

	if c.FingerprintLimit, err = parseInt(getenv("FINGERPRINT_LIMIT"), 50); err != nil || c.FingerprintLimit < 0 || c.FingerprintLimit > 1000 {
		errs = append(errs, fmt.Errorf("FINGERPRINT_LIMIT inválido (0 a 1000): %q", getenv("FINGERPRINT_LIMIT")))
	}
	if c.FingerprintWindow, err = parseDuration(getenv("FINGERPRINT_WINDOW"), 10*time.Minute); err != nil || c.FingerprintWindow < time.Minute || c.FingerprintWindow > 24*time.Hour {
		errs = append(errs, fmt.Errorf("FINGERPRINT_WINDOW inválido (1m a 24h): %q", getenv("FINGERPRINT_WINDOW")))
	}

	return c, errors.Join(errs...)
}

// parsePrefix aceita um CIDR ou um IP solto (tratado como /32 ou /128).
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func parseFloat(s string, def float64) (float64, error) {
	if s == "" {
		return def, nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseInt(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
