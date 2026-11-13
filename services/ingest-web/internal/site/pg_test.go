package site

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Teste de integração: precisa de DATABASE_URL (o banco notify, com o dono das
// tabelas) e dos usuários de 02-ingest-roles.sh, que o CI e o Compose criam.
// Quando a visão ingest_site ainda não existe (banco vazio do job go do CI),
// aplica as migrações do portal. INGEST_DATABASE_URL troca a conexão usada
// como notify_ingest.

func TestPGIntegration(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL não definida")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	ensureMigrations(t, admin)

	active := createTenant(t, admin, "active")
	removed := createTenant(t, admin, "removed")
	ids := map[string]string{"live": newUUID(t), "archived": newUUID(t), "ofRemoved": newUUID(t)}
	for _, s := range []struct {
		tenant, id, archived string
	}{
		{active, ids["live"], "NULL"},
		{active, ids["archived"], "now()"},
		{removed, ids["ofRemoved"], "NULL"},
	} {
		_, err := admin.Exec(ctx, `INSERT INTO site (tenant_id, name, domain, umami_website_id, archived_at)
			VALUES ($1, $2, $2, $3, `+s.archived+`)`, s.tenant, s.id, s.id)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, id := range ids {
			_, _ = admin.Exec(ctx, `DELETE FROM site WHERE umami_website_id = $1`, id)
		}
		_, _ = admin.Exec(ctx, `DELETE FROM tenant WHERE id = ANY($1)`, []string{active, removed})
	})

	pool, err := pgxpool.New(ctx, ingestURL(t, adminURL))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := PG{Pool: pool}

	want := map[string]bool{
		ids["live"]:                            true,
		strings.ToUpper(ids["live"]):           true,
		ids["archived"]:                        false,
		ids["ofRemoved"]:                       false,
		"00000000-0000-4000-8000-00000000dead": false,
	}
	for id, w := range want {
		got, err := store.Active(ctx, id)
		if err != nil {
			t.Fatalf("Active(%s): %v", id, err)
		}
		if got != w {
			t.Errorf("Active(%s) = %v, quero %v", id, got, w)
		}
	}

	// notify_ingest só lê a ingest_site.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM ingest_site`).Scan(&n); err != nil {
		t.Fatalf("SELECT na ingest_site: %v", err)
	}
	_, err = pool.Exec(ctx, `SELECT 1 FROM site`)
	assertDenied(t, "SELECT na site", err)
	_, err = pool.Exec(ctx, `SELECT 1 FROM "user"`)
	assertDenied(t, "SELECT na user", err)
	_, err = pool.Exec(ctx, `CREATE TABLE ingest_probe (x int)`)
	assertDenied(t, "CREATE TABLE", err)
}

// ensureMigrations aplica as migrações do portal, em ordem, quando a visão
// ingest_site não existe.
func ensureMigrations(t *testing.T, admin *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT to_regclass('ingest_site') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		return
	}
	files, err := filepath.Glob("../../../../apps/portal/drizzle/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrações do portal não encontradas: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(string(sql), "--> statement-breakpoint") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := admin.Exec(ctx, stmt); err != nil {
				t.Fatalf("%s: %v (rodou infra/compose/postgres/init/02-ingest-roles.sh?)", filepath.Base(f), err)
			}
		}
	}
}

func createTenant(t *testing.T, admin *pgx.Conn, status string) string {
	t.Helper()
	var id string
	slug := "go-" + newUUID(t)
	err := admin.QueryRow(context.Background(),
		`INSERT INTO tenant (name, slug, status) VALUES ($1, $1, $2) RETURNING id`, slug, status,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ingestURL(t *testing.T, adminURL string) string {
	t.Helper()
	if s := os.Getenv("INGEST_DATABASE_URL"); s != "" {
		return s
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("notify_ingest", "notify_ingest")
	return u.String()
}

func assertDenied(t *testing.T, what string, err error) {
	t.Helper()
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "42501" {
		t.Errorf("%s: quero permission denied (42501), veio %v", what, err)
	}
}

func newUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
