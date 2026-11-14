// Package pgtest reúne o que os testes de integração Go precisam do banco
// notify: um banco com as migrações do portal, sem nunca migrar o banco de
// DATABASE_URL por fora do Drizzle, e conexões como os usuários de ingestão.
package pgtest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database devolve a URL do banco onde o teste roda: um banco temporário
// com as migrações do portal, quando o usuário pode criar bancos, ou o próprio
// banco de adminURL, quando o portal já o migrou.
func Database(t *testing.T, adminURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	var canCreate, migrated bool
	err = conn.QueryRow(ctx, `SELECT rolcreatedb OR rolsuper, to_regclass('ingest_site') IS NOT NULL
		FROM pg_roles WHERE rolname = current_user`).Scan(&canCreate, &migrated)
	if err != nil {
		t.Fatal(err)
	}
	if !canCreate {
		if !migrated {
			t.Skip("o banco de DATABASE_URL não tem a visão ingest_site e o usuário não pode criar um banco " +
				"temporário: suba o portal (que aplica as migrações) ou use um usuário com CREATEDB")
		}
		return adminURL
	}

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ingest_site_test_%x", b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			t.Errorf("apagando %s: %v", name, err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("apagando %s: %v", name, err)
		}
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	tmp, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close(ctx)
	applyMigrations(t, tmp)
	return u.String()
}

// applyMigrations aplica as migrações do portal, em ordem, num banco vazio.
func applyMigrations(t *testing.T, db *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	_, here, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(here), "../../../apps/portal/drizzle/*.sql"))
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
			if _, err := db.Exec(ctx, stmt); err != nil {
				t.Fatalf("%s: %v (rodou infra/compose/postgres/init/02-ingest-roles.sh?)", filepath.Base(f), err)
			}
		}
	}
}

// RoleURL troca o usuário e a senha da URL por role/role, como no Compose e
// no CI, ou usa a URL de envVar quando ela existir.
func RoleURL(t *testing.T, adminURL, role, envVar string) string {
	t.Helper()
	if s := os.Getenv(envVar); s != "" {
		return s
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, role)
	return u.String()
}

// AssertDenied confere que err é permission denied (42501).
func AssertDenied(t *testing.T, what string, err error) {
	t.Helper()
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "42501" {
		t.Errorf("%s: quero permission denied (42501), veio %v", what, err)
	}
}

// NewUUID gera um UUID v4.
func NewUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
