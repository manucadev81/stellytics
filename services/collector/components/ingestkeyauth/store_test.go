package ingestkeyauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/romulospl/notify/services/internal/pgtest"
)

// Teste de integração do Store com a visão real ingest_key_active, como o
// usuário notify_collector. Precisa de DATABASE_URL (veja internal/pgtest).
// COLLECTOR_DATABASE_URL troca a conexão usada como notify_collector.
func TestPGStoreIntegration(t *testing.T) {
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

	userID := createUser(t, admin)
	active := createTenant(t, admin, "active")
	removed := createTenant(t, admin, "removed")
	svc := createService(t, admin, active, false)
	archived := createService(t, admin, active, true)
	svcOfRemoved := createService(t, admin, removed, false)

	good := createKey(t, admin, userID, active, svc, false)
	revoked := createKey(t, admin, userID, active, svc, true)
	ofArchived := createKey(t, admin, userID, active, archived, false)
	ofRemoved := createKey(t, admin, userID, removed, svcOfRemoved, false)

	pool, err := pgxpool.New(ctx, pgtest.RoleURL(t, adminURL, "notify_collector", "COLLECTOR_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	a := newAuthenticator(pgStore{pool: pool}, time.Minute, zap.NewNop())

	ctx2, err := a.Authenticate(ctx, header("Authorization", "Bearer "+good))
	if err != nil {
		t.Fatalf("chave ativa recusada: %v", err)
	}
	if got := attr(ctx2, AttrTenantID); got != active {
		t.Errorf("tenant = %q, quero %q", got, active)
	}
	if got := attr(ctx2, AttrServiceID); got != svc {
		t.Errorf("serviço = %q, quero %q", got, svc)
	}
	for name, key := range map[string]string{"revogada": revoked, "de serviço arquivado": ofArchived, "de tenant removido": ofRemoved} {
		if _, err := a.Authenticate(ctx, header("Authorization", "Bearer "+key)); err == nil {
			t.Errorf("chave %s aceita", name)
		}
	}

	// notify_collector só lê a visão.
	_, err = pool.Exec(ctx, `SELECT 1 FROM ingest_key LIMIT 1`)
	pgtest.AssertDenied(t, "SELECT na ingest_key", err)
}

// Último uso (migração 0008): a função grava o dia, uma vez por dia, só para
// chave ativa, e o notify_collector não ganha mais nada na tabela.
func TestPGStoreTouch(t *testing.T) {
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
	t.Cleanup(func() { admin.Close(context.Background()) })

	userID := createUser(t, admin)
	active := createTenant(t, admin, "active")
	removed := createTenant(t, admin, "removed")
	svc := createService(t, admin, active, false)
	svcOfRemoved := createService(t, admin, removed, false)
	good := createKey(t, admin, userID, active, svc, false)
	revoked := createKey(t, admin, userID, active, svc, true)
	ofRemoved := createKey(t, admin, userID, removed, svcOfRemoved, false)
	prefix := func(key string) string { return key[4:12] }

	pool, err := pgxpool.New(ctx, pgtest.RoleURL(t, adminURL, "notify_collector", "COLLECTOR_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := pgStore{pool: pool}

	lastUsed := func(key string) (d *string, xmin string) {
		t.Helper()
		if err := admin.QueryRow(ctx,
			`SELECT last_used_on::text, xmin::text FROM ingest_key WHERE prefix = $1`, prefix(key),
		).Scan(&d, &xmin); err != nil {
			t.Fatal(err)
		}
		return d, xmin
	}
	var today string
	if err := admin.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'UTC')::date::text`).Scan(&today); err != nil {
		t.Fatal(err)
	}

	if d, _ := lastUsed(good); d != nil {
		t.Fatalf("last_used_on já preenchida: %s", *d)
	}
	if err := store.Touch(ctx, prefix(good)); err != nil {
		t.Fatal(err)
	}
	d, xmin := lastUsed(good)
	if d == nil || *d != today {
		t.Fatalf("last_used_on = %v, quero %s", d, today)
	}
	// Segunda chamada no mesmo dia: a linha não é reescrita.
	if err := store.Touch(ctx, prefix(good)); err != nil {
		t.Fatal(err)
	}
	if _, xmin2 := lastUsed(good); xmin2 != xmin {
		t.Errorf("a linha foi reescrita no mesmo dia (xmin %s -> %s)", xmin, xmin2)
	}

	// Revogada, de tenant removido e prefixo desconhecido: nada é gravado.
	for name, key := range map[string]string{"revogada": revoked, "de tenant removido": ofRemoved} {
		if err := store.Touch(ctx, prefix(key)); err != nil {
			t.Fatal(err)
		}
		if d, _ := lastUsed(key); d != nil {
			t.Errorf("chave %s foi atualizada: %s", name, *d)
		}
	}
	if err := store.Touch(ctx, "zzzzzzzz"); err != nil {
		t.Fatal(err)
	}

	// Menor privilégio: nenhuma outra escrita ou leitura na tabela ou fora dela.
	for what, q := range map[string]string{
		"UPDATE de last_used_on":     `UPDATE ingest_key SET last_used_on = NULL`,
		"UPDATE de revoked_at":       `UPDATE ingest_key SET revoked_at = NULL`,
		"UPDATE de key_hash":         `UPDATE ingest_key SET key_hash = key_hash`,
		"DELETE":                     `DELETE FROM ingest_key`,
		"SELECT de key_hash":         `SELECT key_hash FROM ingest_key`,
		"UPDATE em tenant":           `UPDATE tenant SET status = 'active'`,
		"SELECT em operator_api_key": `SELECT 1 FROM operator_api_key`,
		"UPDATE em api_service":      `UPDATE api_service SET name = name`,
	} {
		_, err := pool.Exec(ctx, q)
		pgtest.AssertDenied(t, what, err)
	}
}

func attr(ctx context.Context, name string) string {
	v, _ := clientAuth(ctx).GetAttribute(name).(string)
	return v
}

// createUser cria o autor das chaves. Chamada antes dos tenants, a limpeza
// dele roda por último, depois que as chaves que o citam já saíram.
func createUser(t *testing.T, db *pgx.Conn) string {
	t.Helper()
	id := pgtest.NewUUID(t)
	if _, err := db.Exec(context.Background(),
		`INSERT INTO "user" (id, name, email, email_verified) VALUES ($1, 'Teste', $1 || '@collector.invalid', true)`, id,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id) })
	return id
}

func createTenant(t *testing.T, db *pgx.Conn, status string) string {
	t.Helper()
	var id string
	slug := "collector-" + pgtest.NewUUID(t)
	if err := db.QueryRow(context.Background(),
		`INSERT INTO tenant (name, slug, status) VALUES ($1, $1, $2) RETURNING id`, slug, status,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.Exec(ctx, `DELETE FROM ingest_key WHERE tenant_id = $1`, id)
		_, _ = db.Exec(ctx, `DELETE FROM api_service WHERE tenant_id = $1`, id)
		_, _ = db.Exec(ctx, `DELETE FROM tenant WHERE id = $1`, id)
	})
	return id
}

func createService(t *testing.T, db *pgx.Conn, tenantID string, archived bool) string {
	t.Helper()
	var id string
	var archivedAt any
	if archived {
		archivedAt = time.Now()
	}
	if err := db.QueryRow(context.Background(),
		`INSERT INTO api_service (tenant_id, name, archived_at) VALUES ($1, $2, $3) RETURNING id`,
		tenantID, "api-"+pgtest.NewUUID(t)[:8], archivedAt,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// createKey grava uma chave no formato do portal (#59) e devolve a chave completa.
func createKey(t *testing.T, db *pgx.Conn, userID, tenantID, serviceID string, revoked bool) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, 8+32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	prefix := make([]byte, 8)
	for i := range prefix {
		prefix[i] = alphabet[b[i]%32]
	}
	key := fmt.Sprintf("ntf_%s_%s", prefix, base64.RawURLEncoding.EncodeToString(b[8:]))
	hash := sha256.Sum256([]byte(key))
	var revokedAt any
	if revoked {
		revokedAt = time.Now()
	}
	if _, err := db.Exec(context.Background(),
		`INSERT INTO ingest_key (api_service_id, tenant_id, prefix, key_hash, created_by, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`, serviceID, tenantID, string(prefix), hash[:], userID, revokedAt,
	); err != nil {
		t.Fatal(err)
	}
	return key
}
