//go:build e2e

// Isolamento entre tenants na ingestão de API (#20 e #21; spec do E2, seções
// 6 e 8): mudanças feitas DEPOIS que a chave já foi aceita valem em até o
// cache do coletor (INGEST_KEY_CACHE_TTL: no máximo 60 s, 5 s no Compose).
//
//   - chave revogada: passa a ser recusada;
//   - tenant removido pelo operador: a chave dele passa a ser recusada, e volta
//     a valer quando o tenant é reativado (D6).
//
// E o caso que depende do exporter (#31): chave de A mandando dados que dizem
// ser de B é gravada como A.
//
// O status muda direto no banco, como fazem as funções do portal (testadas em
// apps/portal/test/isolation.test.ts). A recusa é 401 (HTTP) e Unauthenticated
// (gRPC), a mesma de uma chave inexistente.
package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// keyCacheTTL é o INGEST_KEY_CACHE_TTL do coletor no Compose.
var keyCacheTTL = func() time.Duration {
	d, err := time.ParseDuration(env("COLLECTOR_KEY_CACHE_TTL", "5s"))
	if err != nil {
		panic(err)
	}
	return d
}()

func TestRevokedKeyIsRefusedWithinCacheTTL(t *testing.T) {
	db, user := isolationDB(t)
	key, _, _ := createKey(t, db, user, "active")
	expectAccepted(t, key)

	exec(t, db, `UPDATE ingest_key SET revoked_at = now() WHERE prefix = $1`, prefixOf(key))
	time.Sleep(keyCacheTTL + time.Second)
	expectRefused(t, key)
}

func TestRemovedTenantIsRefusedWithinCacheTTL(t *testing.T) {
	db, user := isolationDB(t)
	key, _, _ := createKey(t, db, user, "active")
	other, _, _ := createKey(t, db, user, "active")
	expectAccepted(t, key)

	setTenantStatus(t, db, key, "removed")
	time.Sleep(keyCacheTTL + time.Second)
	expectRefused(t, key)
	// Os outros tenants continuam.
	expectAccepted(t, other)

	setTenantStatus(t, db, key, "active")
	time.Sleep(keyCacheTTL + time.Second)
	expectAccepted(t, key)
}

// Chave de A mandando dados que dizem ser de B (spec do E2, seção 8): tudo é
// gravado como A, e B fica sem nenhuma linha. A alegação vai em todos os
// níveis que o OTLP permite (recurso, escopo e ponto ou span), com os IDs
// reais de B, em dois recursos no mesmo pedido, por HTTP (métricas) e por gRPC
// (spans, que o coletor transforma em métrica).
func TestKeyOfAClaimingBIsStoredAsA(t *testing.T) {
	db, user := isolationDB(t)
	keyA, tenantA, serviceA := createKey(t, db, user, "active")
	keyC, tenantC, serviceC := createKey(t, db, user, "active")
	_, tenantB, serviceB := createKey(t, db, user, "active")

	md := metrics() // 1 requisição em /ledgers/{id}, ver e2e_test.go
	rm := md.ResourceMetrics().At(0)
	claim(rm.Resource().Attributes(), tenantB, serviceB)
	claim(rm.ScopeMetrics().At(0).Scope().Attributes(), tenantB, serviceB)
	claim(rm.ScopeMetrics().At(0).Metrics().At(0).Histogram().DataPoints().At(0).Attributes(), tenantB, serviceB)
	rm.CopyTo(md.ResourceMetrics().AppendEmpty())
	if code := postHTTPWith(t, "Bearer "+keyA, md); code != http.StatusOK {
		t.Fatalf("métricas de A: status %d", code)
	}

	// Outro tenant (C), só com spans: um serviço que manda métricas tem os
	// spans ignorados (D9 do spec do E5), então o caminho dos spans precisa de
	// um serviço próprio.
	td := spans("/ledgers/7", 3)
	rs := td.ResourceSpans().At(0)
	claim(rs.Resource().Attributes(), tenantB, serviceB)
	claim(rs.ScopeSpans().At(0).Scope().Attributes(), tenantB, serviceB)
	for i := range rs.ScopeSpans().At(0).Spans().Len() {
		claim(rs.ScopeSpans().At(0).Spans().At(i).Attributes(), tenantB, serviceB)
	}
	if err := exportSpansGRPC(t, "Bearer "+keyC, td); err != nil {
		t.Fatalf("spans de C: %v", err)
	}

	wantRows(t, db, tenantA, serviceA, []row{{"/ledgers/{id}", "GET", 200, 2}})
	wantRows(t, db, tenantC, serviceC, []row{{"/ledgers/{id}", "GET", 200, 3}})
	// O batch grava em até 200 ms; tudo que viria para B já teria chegado.
	time.Sleep(time.Second)
	var ofB int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM api_metric_1m WHERE tenant_id = $1 OR api_service_id = $2`,
		tenantB, serviceB).Scan(&ofB); err != nil {
		t.Fatal(err)
	}
	if ofB != 0 {
		t.Errorf("%d linhas gravadas no tenant ou no serviço de B", ofB)
	}
}

// claim põe nos atributos os nomes que um cliente usaria para dizer que é
// outro tenant e outro serviço.
func claim(attrs pcommon.Map, tenantID, serviceID string) {
	for _, k := range []string{"notify.tenant.id", "tenant.id", "tenant_id"} {
		attrs.PutStr(k, tenantID)
	}
	attrs.PutStr("notify.api_service.id", serviceID)
	attrs.PutStr("service.name", serviceID)
}

func isolationDB(t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	ctx := context.Background()
	db, err := pgx.Connect(ctx, notifyDB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	user := randomText(t, 16)
	exec(t, db, `INSERT INTO "user" (id, name, email, email_verified) VALUES ($1, 'e2e', $1 || '@collector.invalid', true)`, user)
	// Depois das chaves e dos tenants (createKey registra a limpeza deles depois,
	// então ela roda antes desta).
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, user) })
	return db, user
}

// prefixOf tira o prefixo público de ntf_<prefixo>_<segredo>.
func prefixOf(key string) string { return key[4:12] }

func setTenantStatus(t *testing.T, db *pgx.Conn, key, status string) {
	t.Helper()
	tag, err := db.Exec(context.Background(),
		`UPDATE tenant SET status = $2, status_changed_at = now()
		 WHERE id = (SELECT tenant_id FROM ingest_key WHERE prefix = $1)`, prefixOf(key), status)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("tenant da chave %s não encontrado", prefixOf(key))
	}
}

func expectAccepted(t *testing.T, key string) {
	t.Helper()
	if code := postHTTP(t, "Bearer "+key); code != http.StatusOK {
		t.Fatalf("HTTP: status = %d, quero 200", code)
	}
	if err := exportGRPC(t, "Bearer "+key); err != nil {
		t.Fatalf("gRPC: chave recusada: %v", err)
	}
}

func expectRefused(t *testing.T, key string) {
	t.Helper()
	if code := postHTTP(t, "Bearer "+key); code != http.StatusUnauthorized {
		t.Fatalf("HTTP: status = %d, quero 401", code)
	}
	if err := exportGRPC(t, "Bearer "+key); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("gRPC: err = %v, quero Unauthenticated", err)
	}
}
