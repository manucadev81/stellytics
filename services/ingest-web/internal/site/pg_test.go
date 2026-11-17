package site

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/romulospl/notify/services/internal/pgtest"
)

// Teste de integração: precisa de DATABASE_URL (o banco stellytics, com o dono das
// tabelas) e dos usuários de 02-ingest-roles.sh, que o CI e o Compose criam.
// INGEST_DATABASE_URL troca a conexão usada como stellytics_ingest.
//
// O teste nunca aplica migrações no banco de DATABASE_URL: elas são do portal,
// que as registra no Drizzle, e aplicá-las por fora faria o portal falhar ao
// migrar depois. Com permissão de criar banco (o CI), ele cria um banco
// temporário, aplica as migrações lá e o apaga no fim. Sem essa permissão (o
// usuário stellytics do Compose), usa o banco de DATABASE_URL se o portal já o
// migrou, e pula o teste se não.

func TestPGIntegration(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL não definida")
	}
	ctx := context.Background()
	adminURL = pgtest.Database(t, adminURL)

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	// Fecha a conexão só depois das limpezas (t.Cleanup roda depois dos defer).
	t.Cleanup(func() { admin.Close(context.Background()) })

	active := createTenant(t, admin, "active")
	removed := createTenant(t, admin, "removed")
	ids := map[string]string{"live": pgtest.NewUUID(t), "archived": pgtest.NewUUID(t), "ofRemoved": pgtest.NewUUID(t)}
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

	pool, err := pgxpool.New(ctx, pgtest.RoleURL(t, adminURL, "stellytics_ingest", "INGEST_DATABASE_URL"))
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

	// stellytics_ingest só lê a ingest_site.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM ingest_site`).Scan(&n); err != nil {
		t.Fatalf("SELECT na ingest_site: %v", err)
	}
	_, err = pool.Exec(ctx, `SELECT 1 FROM site`)
	pgtest.AssertDenied(t, "SELECT na site", err)
	_, err = pool.Exec(ctx, `SELECT 1 FROM "user"`)
	pgtest.AssertDenied(t, "SELECT na user", err)
	_, err = pool.Exec(ctx, `CREATE TABLE ingest_probe (x int)`)
	pgtest.AssertDenied(t, "CREATE TABLE", err)
}

func createTenant(t *testing.T, admin *pgx.Conn, status string) string {
	t.Helper()
	var id string
	slug := "go-" + pgtest.NewUUID(t)
	err := admin.QueryRow(context.Background(),
		`INSERT INTO tenant (name, slug, status) VALUES ($1, $1, $2) RETURNING id`, slug, status,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
