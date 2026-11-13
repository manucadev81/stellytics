//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Gravação de sessão (replays) e heatmaps do Umami 3.4.1 (#62, docs/umami.md):
// só são gravados pela rota /api/record, e só para website com recorder_enabled.
// O teste cria o site como o portal cria, manda page views reais pelo gateway,
// tenta gravar replay e heatmap pelo gateway e direto no Umami, e confere que
// as tabelas session_replay, session_replay_saved e heatmap_event ficam vazias.
func TestRecorderRecordsNothing(t *testing.T) {
	ctx := context.Background()

	token := umamiLogin(t)
	websiteID := umamiCreateWebsite(t, token)
	t.Cleanup(func() { umamiDo(t, token, http.MethodDelete, "/api/websites/"+websiteID, nil, nil) })

	var site struct {
		RecorderEnabled bool            `json:"recorderEnabled"`
		ReplayConfig    *map[string]any `json:"replayConfig"`
	}
	umamiDo(t, token, http.MethodGet, "/api/websites/"+websiteID, nil, &site)
	if site.RecorderEnabled || site.ReplayConfig != nil {
		t.Fatalf("site novo com gravação ligada: recorderEnabled=%v replayConfig=%v",
			site.RecorderEnabled, site.ReplayConfig)
	}

	registerSite(t, connect(t, notifyDB), websiteID)
	time.Sleep(siteCacheTTL + time.Second)
	start := time.Now().Add(-time.Minute)

	for _, path := range []string{"/", "/docs", "/docs/replay"} {
		if code := sendEvent(t, websiteID, path); code != http.StatusNoContent {
			t.Fatalf("page view %s: status = %d, quero 204", path, code)
		}
	}

	// O gateway só tem /e, /c, /s/ e /healthz: a rota de gravação não existe nele.
	for _, path := range []string{"/api/record", "/record"} {
		if code := post(t, gatewayURL+path, recordBody(websiteID), nil); code != http.StatusNotFound {
			t.Errorf("gateway POST %s = %d, quero 404", path, code)
		}
	}

	// Mesmo com acesso direto ao Umami (só na rede interna), o site não grava:
	// o /api/send devolve o token de sessão que o /api/record exige.
	cache := umamiSessionToken(t, websiteID)
	for _, body := range []string{recordBody(websiteID), heatmapBody(websiteID)} {
		var out map[string]any
		code := post(t, umamiURL+"/api/record", body, map[string]string{"x-umami-cache": cache}, &out)
		if code != http.StatusOK || out["ok"] != false || out["reason"] != "recorder_disabled" {
			t.Errorf("Umami /api/record = %d %v, quero 200 recorder_disabled", code, out)
		}
	}

	deadline := time.Now().Add(90 * time.Second)
	for umamiPageviews(t, token, websiteID, start) < 3 {
		if time.Now().After(deadline) {
			t.Fatal("os page views não apareceram no Umami")
		}
		time.Sleep(time.Second)
	}

	db, err := pgx.Connect(ctx, umamiDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	for _, q := range []string{
		`SELECT count(*) FROM session_replay`,
		`SELECT count(*) FROM session_replay_saved`,
		`SELECT count(*) FROM heatmap_event`,
		`SELECT count(*) FROM website WHERE recorder_enabled OR replay_config IS NOT NULL`,
	} {
		var n int
		if err := db.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s = %d, quero 0", q, n)
		}
	}
}

// umamiSessionToken manda um page view direto ao /api/send do Umami e devolve o
// token de sessão (cache) da resposta.
func umamiSessionToken(t *testing.T, websiteID string) string {
	t.Helper()
	body := fmt.Sprintf(`{"type":"event","payload":{"website":%q,"hostname":"e2e.example","url":"/direto",`+
		`"language":"pt-BR","screen":"1920x1080"}}`, websiteID)
	var out struct {
		Cache string `json:"cache"`
	}
	if code := post(t, umamiURL+"/api/send", body, nil, &out); code != http.StatusOK || out.Cache == "" {
		t.Fatalf("Umami /api/send = %d, cache %q", code, out.Cache)
	}
	return out.Cache
}

func recordBody(websiteID string) string {
	return fmt.Sprintf(`{"type":"record","payload":{"website":%q,"events":[{"type":4,"timestamp":%d,`+
		`"data":{"href":"https://e2e.example/","width":1920,"height":1080}}]}}`, websiteID, time.Now().UnixMilli())
}

func heatmapBody(websiteID string) string {
	return fmt.Sprintf(`{"type":"heatmap","payload":{"website":%q,"events":[`+
		`{"type":"click","url":"https://e2e.example/","x":10,"y":20,"pageW":1920,"pageH":3000},`+
		`{"type":"scroll","url":"https://e2e.example/","scrollPct":50}]}}`, websiteID)
}

// post manda um JSON com User-Agent de navegador e decodifica a resposta em
// out, se houver.
func post(t *testing.T, url, body string, headers map[string]string, out ...any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if len(out) > 0 && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out[0]); err != nil {
			t.Fatalf("POST %s: %v", url, err)
		}
	}
	return resp.StatusCode
}
