package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// State é o que o setup cria, gravado em JSON para os outros subcomandos.
type State struct {
	UmamiURL string `json:"umami_url"`
	// Web
	UmamiWebsiteID string `json:"umami_website_id"`
	WebTenantID    string `json:"web_tenant_id"`
	// API
	APITenantID string       `json:"api_tenant_id"`
	Services    []ServiceKey `json:"services"`
	UserID      string       `json:"user_id"`
}

// ServiceKey é um serviço de API do teste com a chave de ingestão dele.
type ServiceKey struct {
	ID   string `json:"id"`
	Mode string `json:"mode"` // "metrics" ou "spans"
	Key  string `json:"key"`
}

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	notifyDB := fs.String("notify-db", envOr("NOTIFY_DATABASE_URL", "postgresql://notify:notify@127.0.0.1:5432/notify"), "banco notify (dono das tabelas)")
	umamiURL := fs.String("umami-url", envOr("UMAMI_URL", "http://127.0.0.1:3001"), "Umami publicado no host")
	out := fs.String("out", "state.json", "arquivo de estado")
	metricsSvcs := fs.Int("metrics-services", 4, "serviços de API que mandam métricas")
	spansSvcs := fs.Int("spans-services", 2, "serviços de API que mandam só spans")
	_ = fs.Parse(args)

	ctx := context.Background()
	db, err := pgx.Connect(ctx, *notifyDB)
	if err != nil {
		return err
	}
	defer db.Close(ctx)

	st := State{UmamiURL: *umamiURL, UserID: "loadtest-" + randStr(8, "abcdefghijklmnopqrstuvwxyz0123456789")}
	if _, err := db.Exec(ctx, `INSERT INTO "user" (id, name, email, email_verified) VALUES ($1, 'loadtest', $2, true)`,
		st.UserID, st.UserID+"@example.invalid"); err != nil {
		return err
	}

	// Web: site no Umami e tenant com esse site no banco notify.
	tok, err := umamiLogin(*umamiURL)
	if err != nil {
		return err
	}
	var site struct {
		ID string `json:"id"`
	}
	if err := umamiDo(*umamiURL, tok, http.MethodPost, "/api/websites",
		map[string]string{"name": st.UserID, "domain": "loadtest.example"}, &site); err != nil || site.ID == "" {
		return fmt.Errorf("criando site no Umami: %v", err)
	}
	st.UmamiWebsiteID = site.ID
	if err := db.QueryRow(ctx, `INSERT INTO tenant (name, slug, status) VALUES ($1, $1, 'active') RETURNING id`, st.UserID+"-web").Scan(&st.WebTenantID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `INSERT INTO site (tenant_id, name, domain, umami_website_id) VALUES ($1, 'loadtest', 'loadtest.example', $2)`,
		st.WebTenantID, st.UmamiWebsiteID); err != nil {
		return err
	}

	// API: um tenant com vários serviços, cada um com a própria chave.
	if err := db.QueryRow(ctx, `INSERT INTO tenant (name, slug, status) VALUES ($1, $1, 'active') RETURNING id`, st.UserID+"-api").Scan(&st.APITenantID); err != nil {
		return err
	}
	mk := func(mode string, i int) error {
		var svc string
		if err := db.QueryRow(ctx, `INSERT INTO api_service (tenant_id, name) VALUES ($1, $2) RETURNING id`,
			st.APITenantID, fmt.Sprintf("%s-%d", mode, i)).Scan(&svc); err != nil {
			return err
		}
		prefix := randStr(8, "abcdefghijklmnopqrstuvwxyz234567")
		secret := randStr(43, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-")
		key := "ntf_" + prefix + "_" + secret
		if _, err := db.Exec(ctx, `INSERT INTO ingest_key (api_service_id, tenant_id, prefix, key_hash, created_by)
			VALUES ($1, $2, $3, sha256(convert_to($4, 'UTF8')), $5)`, svc, st.APITenantID, prefix, key, st.UserID); err != nil {
			return err
		}
		st.Services = append(st.Services, ServiceKey{ID: svc, Mode: mode, Key: key})
		return nil
	}
	for i := 0; i < *metricsSvcs; i++ {
		if err := mk("metrics", i); err != nil {
			return err
		}
	}
	for i := 0; i < *spansSvcs; i++ {
		if err := mk("spans", i); err != nil {
			return err
		}
	}

	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(*out, b, 0o600); err != nil {
		return err
	}
	fmt.Printf("setup ok: site %s, tenant de API %s, %d serviços; estado em %s\n", st.UmamiWebsiteID, st.APITenantID, len(st.Services), *out)
	return nil
}

func runTeardown(args []string) error {
	fs := flag.NewFlagSet("teardown", flag.ExitOnError)
	notifyDB := fs.String("notify-db", envOr("NOTIFY_DATABASE_URL", "postgresql://notify:notify@127.0.0.1:5432/notify"), "banco notify")
	in := fs.String("state", "state.json", "arquivo de estado")
	_ = fs.Parse(args)
	st, err := loadState(*in)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := pgx.Connect(ctx, *notifyDB)
	if err != nil {
		return err
	}
	defer db.Close(ctx)
	if tok, err := umamiLogin(st.UmamiURL); err == nil {
		_ = umamiDo(st.UmamiURL, tok, http.MethodDelete, "/api/websites/"+st.UmamiWebsiteID, nil, nil)
	}
	for _, q := range []string{
		`DELETE FROM api_metric_1m WHERE tenant_id = $1`,
		`DELETE FROM api_metric_1h WHERE tenant_id = $1`,
		`DELETE FROM api_metric_1d WHERE tenant_id = $1`,
		`DELETE FROM ingest_key WHERE tenant_id = $1`,
		`DELETE FROM api_service WHERE tenant_id = $1`,
		`DELETE FROM site WHERE tenant_id = $1`,
		`DELETE FROM tenant WHERE id = $1`,
	} {
		for _, id := range []string{st.APITenantID, st.WebTenantID} {
			if _, err := db.Exec(ctx, q, id); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
	}
	_, err = db.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, st.UserID)
	return err
}

func loadState(path string) (State, error) {
	var st State
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

func umamiLogin(base string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := umamiDo(base, "", http.MethodPost, "/api/auth/login",
		map[string]string{"username": envOr("UMAMI_USER", "admin"), "password": envOr("UMAMI_PASSWORD", "umami")}, &out)
	if err == nil && out.Token == "" {
		err = fmt.Errorf("login sem token")
	}
	return out.Token, err
}

func umamiDo(base, token, method, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func randStr(n int, alphabet string) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
