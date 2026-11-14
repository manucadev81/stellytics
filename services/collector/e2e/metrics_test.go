//go:build e2e

package e2e

// #31 contra o contêiner: o que chega ao api_metric_1m.

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const account = "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7"

type row struct {
	route, method string
	status        int
	count         int64
}

func TestCollectorWritesMetrics(t *testing.T) {
	ctx := context.Background()
	db, err := pgx.Connect(ctx, notifyDB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	user := randomText(t, 16)
	exec(t, db, `INSERT INTO "user" (id, name, email, email_verified) VALUES ($1, 'e2e', $1 || '@collector.invalid', true)`, user)
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, user) })

	// Serviço A manda a métrica (delta) e também spans: só a métrica conta (D9).
	keyA, tenantA, serviceA := createKey(t, db, user, "active")
	md := metrics() // 1 requisição em /ledgers/{id}, ver e2e_test.go
	md.ResourceMetrics().At(0).Resource().Attributes().PutStr("notify.tenant.id", "forjado")
	if code := postHTTPWith(t, "Bearer "+keyA, md); code != http.StatusOK {
		t.Fatalf("métrica: status %d", code)
	}
	// Métricas e traces passam por batches separados; a regra vale depois que a
	// métrica do serviço chegou ao exporter (spec, seção 2).
	wantRows(t, db, tenantA, serviceA, []row{{"/ledgers/{id}", "GET", 200, 1}})
	if err := exportSpansGRPC(t, "Bearer "+keyA, spans("/ledgers/9", 5)); err != nil {
		t.Fatalf("spans do serviço A: %v", err)
	}

	// Serviço B só manda traces: os spans de servidor HTTP viram métrica, com
	// a rota normalizada; spans de cliente e logs não deixam nada.
	keyB, tenantB, serviceB := createKey(t, db, user, "active")
	if err := exportSpansGRPC(t, "Bearer "+keyB, spans("/accounts/"+account+"/payments/", 2)); err != nil {
		t.Fatalf("spans do serviço B: %v", err)
	}
	if code := postLogs(t, "Bearer "+keyB); code != http.StatusOK {
		t.Fatalf("logs: status %d", code)
	}

	wantRows(t, db, tenantB, serviceB, []row{{"/accounts/{address}/payments", "GET", 200, 2}})
	// Os spans do serviço A já passaram pelo exporter junto com os do B (o batch
	// grava em até 200 ms); continuam sem contar.
	time.Sleep(time.Second)
	wantRows(t, db, tenantA, serviceA, []row{{"/ledgers/{id}", "GET", 200, 1}})
}

func wantRows(t *testing.T, db *pgx.Conn, tenantID, serviceID string, want []row) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var got []row
	for {
		got = got[:0]
		rows, err := db.Query(context.Background(), `
			SELECT route, method, status_code, sum(request_count)::bigint
			FROM api_metric_1m WHERE tenant_id = $1 AND api_service_id = $2
			GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, tenantID, serviceID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.route, &r.method, &r.status, &r.count); err != nil {
				t.Fatal(err)
			}
			got = append(got, r)
		}
		if len(got) == len(want) && equalRows(got, want) || time.Now().After(deadline) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !equalRows(got, want) {
		t.Errorf("linhas do serviço = %+v, quero %+v", got, want)
	}
	// Nada de outro tenant nem dos consolidados (são do job da #34).
	var other int
	if err := db.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM api_metric_1h WHERE tenant_id = $1) + (SELECT count(*) FROM api_metric_1d WHERE tenant_id = $1)`,
		tenantID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != 0 {
		t.Errorf("o coletor gravou %d linhas nos consolidados", other)
	}
}

func equalRows(a, b []row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spans monta n spans de servidor HTTP no caminho dado, mais um de cliente.
func spans(path string, n int) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "e2e")
	ss := rs.ScopeSpans().AppendEmpty().Spans()
	now := time.Now()
	for range n {
		sp := ss.AppendEmpty()
		sp.SetKind(ptrace.SpanKindServer)
		sp.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-30 * time.Millisecond)))
		sp.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
		sp.Attributes().PutStr("http.request.method", "GET")
		sp.Attributes().PutStr("url.path", path)
		sp.Attributes().PutInt("http.response.status_code", 200)
		sp.Attributes().PutStr("enduser.id", "alice")
	}
	client := ss.AppendEmpty()
	client.SetKind(ptrace.SpanKindClient)
	client.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-time.Millisecond)))
	client.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
	client.Attributes().PutStr("http.request.method", "GET")
	return td
}

func exportSpansGRPC(t *testing.T, auth string, td ptrace.Traces) error {
	t.Helper()
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", auth)
	_, err = ptraceotlp.NewGRPCClient(conn).Export(ctx, ptraceotlp.NewExportRequestFromTraces(td))
	return err
}

func postHTTPWith(t *testing.T, auth string, md pmetric.Metrics) int {
	t.Helper()
	return post(t, "/v1/metrics", auth, func() ([]byte, error) {
		return pmetricotlpRequest(md)
	})
}

func postLogs(t *testing.T, auth string) int {
	t.Helper()
	ld := plog.NewLogs()
	rec := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.Body().SetStr("GET /accounts/" + account + " 200 user=alice")
	return post(t, "/v1/logs", auth, plogotlp.NewExportRequestFromLogs(ld).MarshalProto)
}

func post(t *testing.T, path, auth string, body func() ([]byte, error)) int {
	t.Helper()
	b, err := body()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, httpURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("coletor fora do ar: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
