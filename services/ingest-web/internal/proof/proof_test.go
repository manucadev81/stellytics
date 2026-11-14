package proof

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

const site = "6f1c2b9e-3a4d-4c8e-9f00-1234567890ab"

func newIssuer(now time.Time) *Issuer {
	i := New([]byte("chave-de-teste-com-pelo-menos-32-bytes"), 10*time.Minute, 8)
	i.now = func() time.Time { return now }
	return i
}

func TestVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	issuer := newIssuer(now)
	c := issuer.Challenge(site)
	if c.Bits != 8 {
		t.Fatalf("Bits = %d", c.Bits)
	}
	nonce := Solve(c.Token, c.Bits)

	if err := issuer.Verify(site, c.Token, nonce); err != nil {
		t.Fatalf("prova válida recusada: %v", err)
	}

	// Achar um nonce que não resolve.
	bad := "0"
	for Solved(c.Token, bad, c.Bits) {
		bad += "1"
	}
	other := New([]byte("outra-chave-com-pelo-menos-32-bytes!!"), 10*time.Minute, 8)
	other.now = issuer.now
	forged := other.Challenge(site)

	raw, _ := base64.RawURLEncoding.DecodeString(c.Token)
	raw[10] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	cases := []struct {
		name                  string
		website, token, nonce string
		want                  error
	}{
		{"sem prova", site, "", "", ErrMissing},
		{"sem nonce", site, c.Token, "", ErrMalformed},
		{"nonce não numérico", site, c.Token, "abc", ErrMalformed},
		{"nonce longo", site, c.Token, "123456789012345678901", ErrMalformed},
		{"token curto", site, "abc", nonce, ErrMalformed},
		{"token de outra chave", site, forged.Token, Solve(forged.Token, 8), ErrSignature},
		{"token alterado", site, tampered, Solve(tampered, 8), ErrSignature},
		{"outro site", "0a0b0c0d-0e0f-4000-8000-000000000001", c.Token, nonce, ErrSignature},
		{"trabalho insuficiente", site, c.Token, bad, ErrWork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := issuer.Verify(tc.website, tc.token, tc.nonce); !errors.Is(err, tc.want) {
				t.Errorf("Verify = %v, quero %v", err, tc.want)
			}
		})
	}

	t.Run("site em maiúsculas", func(t *testing.T) {
		if err := issuer.Verify("6F1C2B9E-3A4D-4C8E-9F00-1234567890AB", c.Token, nonce); err != nil {
			t.Errorf("Verify = %v", err)
		}
	})
}

func TestVerifyExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	c := newIssuer(now).Challenge(site)
	nonce := Solve(c.Token, c.Bits)

	cases := map[string]struct {
		at   time.Time
		want error
	}{
		"dentro do prazo":   {now.Add(9 * time.Minute), nil},
		"expirado":          {now.Add(11 * time.Minute), ErrExpired},
		"emitido no futuro": {now.Add(-2 * time.Minute), ErrExpired},
		"relógio adiantado": {now.Add(-30 * time.Second), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := newIssuer(tc.at).Verify(site, c.Token, nonce); !errors.Is(err, tc.want) {
				t.Errorf("Verify = %v, quero %v", err, tc.want)
			}
		})
	}
}

func TestLeadingZeros(t *testing.T) {
	cases := []struct {
		in   []byte
		want int
	}{
		{[]byte{0x80}, 0},
		{[]byte{0x01}, 7},
		{[]byte{0x00, 0x10}, 11},
		{[]byte{0x00, 0x00}, 16},
	}
	for _, c := range cases {
		if got := LeadingZeros(c.in); got != c.want {
			t.Errorf("LeadingZeros(%x) = %d, quero %d", c.in, got, c.want)
		}
	}
}
