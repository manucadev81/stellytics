package main

import (
	"crypto/sha256"
	"fmt"
	"math/bits"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/romulospl/notify/services/collector/components/httpmetrics"
)

func TestParseProfile(t *testing.T) {
	got, err := parseProfile("11.6x2m, 116x30s")
	if err != nil || len(got) != 2 || got[0] != (step{11.6, 2 * time.Minute}) || got[1] != (step{116, 30 * time.Second}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"", "10", "0x1m", "10x", "-1x1m", "10xabc"} {
		if _, err := parseProfile(bad); err == nil {
			t.Errorf("%q devia falhar", bad)
		}
	}
}

// solve precisa produzir o que internal/proof.Solved aceita: SHA-256(token+":"+nonce)
// com n bits zero no começo. Os vetores abaixo vêm da mesma regra, conferida aqui à mão.
func TestSolveMeetsDifficulty(t *testing.T) {
	for _, n := range []int{0, 8, 12, 16} {
		nonce := solve("token-de-teste", n)
		sum := sha256.Sum256([]byte("token-de-teste:" + nonce))
		z := 0
		for _, c := range sum {
			if c != 0 {
				z += bits.LeadingZeros8(c)
				break
			}
			z += 8
		}
		if z < n {
			t.Errorf("n=%d: nonce %s tem só %d bits zero", n, nonce, z)
		}
	}
}

func TestVisitorsHaveDistinctTruncatedIPs(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < 20000; i++ { // até 65.536 visitantes têm /24 próprio
		v := newVisitor(i)
		var a, b, c, d int
		if _, err := sscan(v.ip, &a, &b, &c, &d); err != nil {
			t.Fatal(err)
		}
		seen[v.ip[:len(v.ip)-len(itoa(d))-1]]++
	}
	if len(seen) != 20000 {
		t.Errorf("%d blocos /24 distintos para 20000 visitantes", len(seen))
	}
}

func TestMarshalOTLPRoundTrip(t *testing.T) {
	h := map[histKey]*hist{{"/a/{id}", "GET", 200}: {sum: 1.5}}
	h[histKey{"/a/{id}", "GET", 200}].counts[httpmetrics.BucketIndex(0.04)] = 3
	md, err := (&pmetric.ProtoUnmarshaler{}).UnmarshalMetrics(marshalMetrics(h, 10*time.Second))
	if err != nil || md.DataPointCount() != 1 {
		t.Fatalf("métricas: %v, %d pontos", err, md.DataPointCount())
	}
	spans := []spanRec{{route: "/a/{id}", path: "/a/1", method: "GET", status: 200, dur: 0.01, end: time.Now(), useRoute: true}}
	td, err := (&ptrace.ProtoUnmarshaler{}).UnmarshalTraces(marshalSpans(spans))
	if err != nil || td.SpanCount() != 1 {
		t.Fatalf("spans: %v, %d", err, td.SpanCount())
	}
}

func sscan(ip string, a, b, c, d *int) (int, error) { return fmt.Sscanf(ip, "%d.%d.%d.%d", a, b, c, d) }

func itoa(n int) string { return strconv.Itoa(n) }

func TestCheckTargetsRefusesRemote(t *testing.T) {
	local := map[string]string{
		"a": "postgresql://u:p@127.0.0.1:5432/stellytics", "b": "http://localhost:3001", "c": "http://[::1]:4318",
	}
	for name, u := range local {
		if err := checkTargets(false, map[string]string{name: u}); err != nil {
			t.Errorf("%s (%s) devia passar: %v", name, u, err)
		}
	}
	remote := []string{"postgresql://u:p@db.example.com:5432/stellytics", "http://10.0.0.5:3001", "https://umami.example.org", "http://127.0.0.1.evil.example"}
	for _, u := range remote {
		if err := checkTargets(false, map[string]string{"x": u}); err == nil {
			t.Errorf("%s devia ser recusado sem -allow-remote", u)
		}
		if err := checkTargets(true, map[string]string{"x": u}); err != nil {
			t.Errorf("%s devia passar com -allow-remote: %v", u, err)
		}
	}
	if err := checkTargets(true, map[string]string{"x": "::nope"}); err == nil {
		t.Error("URL inválida devia falhar")
	}
}

func TestValidateState(t *testing.T) {
	if err := validateState(State{UserID: "loadtest-abc123"}); err != nil {
		t.Error(err)
	}
	for _, id := range []string{"", "loadtest-", "admin", "loadtest-%", "other-loadtest-x"} {
		if err := validateState(State{UserID: id}); err == nil {
			t.Errorf("user_id %q devia ser recusado", id)
		}
	}
}
