package main

// Testes da #25: o desafio em GET /c e o descarte de eventos sem prova.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/romulospl/notify/services/ingest-web/internal/proof"
)

func TestChallenge(t *testing.T) {
	s := newSetup(t)
	resp, err := http.Get(s.server.URL + "/c?s=" + strings.ToUpper(activeSite))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for k, want := range map[string]string{
		"Content-Type":                "application/json",
		"Cache-Control":               "no-store",
		"Access-Control-Allow-Origin": "*",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, quero %q", k, got, want)
		}
	}
	var c proof.Challenge
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Bits != 4 || c.Token == "" {
		t.Fatalf("desafio = %+v", c)
	}
	if err := testProofs.Verify(activeSite, c.Token, proof.Solve(c.Token, c.Bits)); err != nil {
		t.Errorf("token do desafio não vale para o site: %v", err)
	}

	for _, q := range []string{"", "?s=", "?s=nao-e-uuid"} {
		resp, err := http.Get(s.server.URL + "/c" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("/c%s: status = %d, quero 400", q, resp.StatusCode)
		}
	}
}

func TestEventWithoutValidProofDiscarded(t *testing.T) {
	// Um desafio emitido há 11 minutos pela mesma chave.
	old := proof.New([]byte("chave-de-teste-com-pelo-menos-32-bytes"), 10*time.Minute, 4).
		WithClock(func() time.Time { return time.Now().Add(-11 * time.Minute) }).
		Challenge(activeSite).Token

	valid := testProofs.Challenge(activeSite)
	payload := `"payload":{"website":"` + activeSite + `","url":"/"}`
	otherSite := testProofs.Challenge(pendingSite)
	bad := "0"
	for proof.Solved(valid.Token, bad, valid.Bits) {
		bad += "1"
	}

	cases := map[string]string{
		// Um POST direto, como o curl faria.
		"sem prova":             `{"type":"event",` + payload + `}`,
		"token inventado":       `{"type":"event","p":{"t":"` + strings.Repeat("A", 43) + `","n":"1"},` + payload + `}`,
		"nonce sem trabalho":    `{"type":"event","p":{"t":"` + valid.Token + `","n":"` + bad + `"},` + payload + `}`,
		"token de outro site":   `{"type":"event","p":{"t":"` + otherSite.Token + `","n":"` + proof.Solve(otherSite.Token, 4) + `"},` + payload + `}`,
		"token expirado":        `{"type":"event","p":{"t":"` + old + `","n":"` + proof.Solve(old, 4) + `"},` + payload + `}`,
		"prova em formato ruim": `{"type":"event","p":"abc",` + payload + `}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			resp := post(t, s, "text/plain", body)
			want := http.StatusNoContent
			if name == "prova em formato ruim" {
				want = http.StatusBadRequest
			}
			if resp.StatusCode != want {
				t.Errorf("status = %d, quero %d", resp.StatusCode, want)
			}
			if n := len(s.umami.received()); n != 0 {
				t.Errorf("umami recebeu %d eventos, quero 0", n)
			}
		})
	}

	t.Run("prova válida", func(t *testing.T) {
		s := newSetup(t)
		body := `{"type":"event","p":{"t":"` + valid.Token + `","n":"` + proof.Solve(valid.Token, 4) + `"},` + payload + `}`
		if resp := post(t, s, "text/plain", body); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		got := s.umami.received()
		if len(got) != 1 {
			t.Fatalf("umami recebeu %d eventos, quero 1", len(got))
		}
		if _, ok := got[0].body["p"]; ok {
			t.Error("a prova foi repassada ao Umami")
		}
	})
}
