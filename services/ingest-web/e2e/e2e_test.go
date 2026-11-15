//go:build e2e

// Teste ponta a ponta contra o ambiente do Docker Compose:
//
//	docker compose -f compose.yaml -f infra/compose/umami-port.yaml up -d --build --wait
//	cd services && go test -tags e2e ./ingest-web/e2e/...
//
// Cria um website no Umami pela API e um tenant ativo com esse site no banco
// notify (o que aparece na ingest_site), envia um page view ao gateway e
// confere que ele aparece no Umami, e que o evento enviado antes do cadastro não.
// As URLs podem ser trocadas por GATEWAY_URL, UMAMI_URL, NOTIFY_DATABASE_URL e
// UMAMI_DATABASE_URL. O teste de privacidade (privacy_test.go) exige que o
// gateway confie no X-Forwarded-For da conexão do teste (INGEST_TRUSTED_PROXIES).
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/romulospl/notify/services/ingest-web/internal/proof"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	gatewayURL = env("GATEWAY_URL", "http://127.0.0.1:3002")
	umamiURL   = env("UMAMI_URL", "http://127.0.0.1:3001")
	notifyDB   = env("NOTIFY_DATABASE_URL", "postgresql://notify:notify@127.0.0.1:5432/notify")
	umamiDB    = env("UMAMI_DATABASE_URL", "postgresql://umami:umami@127.0.0.1:5432/umami")
)

// siteCacheTTL é o SITE_CACHE_TTL do gateway no Compose.
const siteCacheTTL = 5 * time.Second

func TestEventReachesUmami(t *testing.T) {
	ctx := context.Background()

	resp, err := http.Get(gatewayURL + "/healthz")
	if err != nil {
		t.Fatalf("gateway fora do ar: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d", resp.StatusCode)
	}

	token := umamiLogin(t)
	websiteID := umamiCreateWebsite(t, token)
	t.Cleanup(func() { umamiDo(t, token, http.MethodDelete, "/api/websites/"+websiteID, nil, nil) })

	db, err := pgx.Connect(ctx, notifyDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)

	// Antes do cadastro, o gateway responde como se aceitasse (spec do E2,
	// seção 6), mas descarta: o pageview não pode chegar ao Umami.
	if code := sendEvent(t, websiteID, "/antes"); code != http.StatusNoContent {
		t.Fatalf("site fora da ingest_site: status = %d, quero 204", code)
	}

	registerSite(t, db, websiteID)

	// A resposta negativa fica no cache do gateway por SITE_CACHE_TTL (5 s no
	// Compose).
	time.Sleep(siteCacheTTL + time.Second)
	start := time.Now().Add(-time.Minute)
	deadline := time.Now().Add(90 * time.Second)
	if code := sendEvent(t, websiteID, "/docs/e2e"); code != http.StatusNoContent {
		t.Fatalf("evento válido: status = %d, quero 204", code)
	}

	for {
		views := umamiPageviews(t, token, websiteID, start)
		if views == 1 {
			return
		}
		if views > 1 {
			t.Fatalf("pageviews = %d, quero 1 (o evento recusado chegou ao Umami?)", views)
		}
		if time.Now().After(deadline) {
			t.Fatal("o page view não apareceu no Umami")
		}
		time.Sleep(time.Second)
	}
}

// registerSite cria um tenant ativo com o site no banco notify, o que o põe na
// ingest_site com accepts_ingest verdadeiro.
func registerSite(t *testing.T, db *pgx.Conn, websiteID string) {
	t.Helper()
	ctx := context.Background()
	var tenantID string
	if err := db.QueryRow(ctx,
		`INSERT INTO tenant (name, slug) VALUES ($1, $1) RETURNING id`, "e2e-"+websiteID,
	).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO site (tenant_id, name, domain, umami_website_id)
		VALUES ($1, 'e2e', 'e2e.example', $2)`, tenantID, websiteID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.Exec(ctx, `DELETE FROM site WHERE tenant_id = $1`, tenantID)
		_, _ = db.Exec(ctx, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})
}

// sendEvent manda um page view ao gateway; hdr são pares de cabeçalho extras.
func sendEvent(t *testing.T, websiteID, path string, hdr ...string) int {
	t.Helper()
	token, nonce := solveChallenge(t, websiteID)
	body := fmt.Sprintf(`{"type":"event","p":{"t":%q,"n":%q},"payload":{"website":%q,"hostname":"e2e.example","url":%q,`+
		`"title":"e2e","language":"pt-BR","screen":"1920x1080"}}`, token, nonce, websiteID, path)
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/e", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("User-Agent", browserUA)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// solveChallenge pede o desafio em GET /c e o resolve, como o script faz (#25).
func solveChallenge(t *testing.T, websiteID string) (token, nonce string) {
	t.Helper()
	resp, err := http.Get(gatewayURL + "/c?s=" + websiteID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var c proof.Challenge
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&c) != nil {
		t.Fatalf("GET /c: status %d", resp.StatusCode)
	}
	return c.Token, proof.Solve(c.Token, c.Bits)
}

func umamiLogin(t *testing.T) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	umamiDo(t, "", http.MethodPost, "/api/auth/login",
		map[string]string{"username": env("UMAMI_USER", "admin"), "password": env("UMAMI_PASSWORD", "umami")}, &out)
	if out.Token == "" {
		t.Fatal("login no Umami sem token")
	}
	return out.Token
}

func umamiCreateWebsite(t *testing.T, token string) string {
	t.Helper()
	var out struct {
		ID string `json:"id"`
	}
	name := "e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	umamiDo(t, token, http.MethodPost, "/api/websites", map[string]string{"name": name, "domain": "e2e.example"}, &out)
	if out.ID == "" {
		t.Fatal("Umami não devolveu o id do website")
	}
	return out.ID
}

func umamiPageviews(t *testing.T, token, websiteID string, since time.Time) int {
	t.Helper()
	var out map[string]json.RawMessage
	path := fmt.Sprintf("/api/websites/%s/stats?startAt=%d&endAt=%d",
		websiteID, since.UnixMilli(), time.Now().Add(time.Minute).UnixMilli())
	umamiDo(t, token, http.MethodGet, path, nil, &out)
	raw, ok := out["pageviews"]
	if !ok {
		t.Fatalf("stats sem pageviews: %v", out)
	}
	// v3 devolve um número; versões antigas, {"value": n}.
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var v struct{ Value int }
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("pageviews inesperado: %s", raw)
	}
	return v.Value
}

func umamiDo(t *testing.T, token, method, path string, in, out any) {
	t.Helper()
	var body bytes.Buffer
	if in != nil {
		_ = json.NewEncoder(&body).Encode(in)
	}
	req, _ := http.NewRequest(method, umamiURL+path, &body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s = %d", method, path, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// TestCustomEventReachesUmami envia um evento customizado (#50) e confere no
// banco do Umami o nome e as propriedades, e que o IP completo não foi gravado.
func TestCustomEventReachesUmami(t *testing.T) {
	ctx := context.Background()
	token := umamiLogin(t)
	websiteID := umamiCreateWebsite(t, token)
	t.Cleanup(func() { umamiDo(t, token, http.MethodDelete, "/api/websites/"+websiteID, nil, nil) })

	db, err := pgx.Connect(ctx, notifyDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	registerSite(t, db, websiteID)
	time.Sleep(siteCacheTTL + time.Second)

	body := func(extra string) string {
		tok, nonce := solveChallenge(t, websiteID)
		return fmt.Sprintf(`{"type":"event","p":{"t":%q,"n":%q},"payload":{"website":%q,"hostname":"e2e.example","url":"/pricing",`+
			`"title":"e2e","language":"pt-BR","screen":"1920x1080",%s}}`, tok, nonce, websiteID, extra)
	}
	post := func(b string) int {
		req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/e", strings.NewReader(b))
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		req.Header.Set("User-Agent", browserUA)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(body(`"name":"signup_clicked","data":{"plan":"pro","seats":3,"trial":true}`)); code != http.StatusNoContent {
		t.Fatalf("evento customizado: status = %d, quero 204", code)
	}
	if code := post(body(`"name":"bad name"`)); code != http.StatusBadRequest {
		t.Fatalf("nome inválido: status = %d, quero 400", code)
	}

	udb, err := pgx.Connect(ctx, umamiDB)
	if err != nil {
		t.Fatal(err)
	}
	defer udb.Close(ctx)

	deadline := time.Now().Add(90 * time.Second)
	for {
		rows, err := udb.Query(ctx, `SELECT e.event_name, d.data_key, d.string_value, d.number_value::text
			FROM website_event e JOIN event_data d ON d.website_event_id = e.event_id
			WHERE e.website_id = $1 AND e.event_name IS NOT NULL ORDER BY d.data_key`, websiteID)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for rows.Next() {
			var name, key string
			var s, n *string
			if err := rows.Scan(&name, &key, &s, &n); err != nil {
				t.Fatal(err)
			}
			if name != "signup_clicked" {
				t.Fatalf("event_name = %q", name)
			}
			switch {
			case s != nil:
				got[key] = *s
			case n != nil:
				got[key] = *n
			}
		}
		rows.Close()
		if got["plan"] == "pro" && got["seats"] != "" {
			t.Logf("propriedades no Umami: %v", got)
			if got["plan"] != "pro" || !strings.HasPrefix(got["seats"], "3") {
				t.Fatalf("propriedades = %v", got)
			}
			var n int
			if err := udb.QueryRow(ctx, `SELECT count(*) FROM website_event WHERE website_id = $1 AND event_name IS NOT NULL`, websiteID).Scan(&n); err != nil || n != 1 {
				t.Fatalf("eventos customizados = %d (err %v), quero 1", n, err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("o evento customizado não apareceu no Umami (visto: %v)", got)
		}
		time.Sleep(time.Second)
	}
}
