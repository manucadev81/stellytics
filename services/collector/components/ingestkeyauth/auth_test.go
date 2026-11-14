package ingestkeyauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.uber.org/zap"
)

const (
	validKey = "ntf_abcdefgh_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	tenantID = "6f1c2b9e-3a4d-4c8e-9f00-1234567890ab"
	service  = "0a0b0c0d-0e0f-4000-8000-000000000001"
)

// fakeStore guarda chaves por prefixo e conta as consultas.
type fakeStore struct {
	keys  map[string]Key
	err   error
	calls atomic.Int32
}

func (f *fakeStore) Lookup(_ context.Context, prefix string) (Key, bool, error) {
	f.calls.Add(1)
	if f.err != nil {
		return Key{}, false, f.err
	}
	k, ok := f.keys[prefix]
	return k, ok, nil
}

func newStore() *fakeStore {
	h := sha256.Sum256([]byte(validKey))
	return &fakeStore{keys: map[string]Key{"abcdefgh": {Hash: h[:], TenantID: tenantID, ServiceID: service}}}
}

func header(name, value string) map[string][]string { return map[string][]string{name: {value}} }

func TestAuthenticateValidKey(t *testing.T) {
	a := newAuthenticator(newStore(), time.Minute, zap.NewNop())
	// HTTP manda o header com a grafia canônica; gRPC, em minúsculas.
	for _, name := range []string{"Authorization", "authorization"} {
		ctx, err := a.Authenticate(context.Background(), header(name, "Bearer "+validKey))
		if err != nil {
			t.Fatalf("%s: chave válida recusada: %v", name, err)
		}
		auth := client.FromContext(ctx).Auth
		if auth == nil || auth.GetAttribute(AttrTenantID) != tenantID || auth.GetAttribute(AttrServiceID) != service ||
			auth.GetAttribute(AttrPrefix) != "abcdefgh" {
			t.Fatalf("%s: auth = %#v", name, auth)
		}
	}
}

func TestAuthenticateRejects(t *testing.T) {
	wrongSecret := validKey[:len(validKey)-1] + "H"
	cases := map[string]map[string][]string{
		"sem header":           {},
		"sem Bearer":           header("Authorization", validKey),
		"outro esquema":        header("Authorization", "Basic "+validKey),
		"formato errado":       header("Authorization", "Bearer ntf_ABCDEFGH_x"),
		"segredo errado":       header("Authorization", "Bearer "+wrongSecret),
		"prefixo desconhecido": header("Authorization", "Bearer ntf_zzzzzzzz_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"),
		"dois valores":         {"Authorization": {"Bearer " + validKey, "Bearer " + validKey}},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			a := newAuthenticator(newStore(), time.Minute, zap.NewNop())
			ctx, err := a.Authenticate(context.Background(), h)
			if !errors.Is(err, errUnauthenticated) {
				t.Fatalf("err = %v, quero errUnauthenticated", err)
			}
			if client.FromContext(ctx).Auth != nil {
				t.Error("contexto recusado levou dados de autenticação")
			}
		})
	}
}

func TestCache(t *testing.T) {
	store := newStore()
	now := time.Unix(1_800_000_000, 0)
	a := newAuthenticator(store, time.Minute, zap.NewNop())
	a.now = func() time.Time { return now }
	auth := func(key string) error {
		_, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+key))
		return err
	}
	wrongSecret := validKey[:len(validKey)-1] + "H"

	for range 3 {
		if err := auth(validKey); err != nil {
			t.Fatal(err)
		}
		if auth(wrongSecret) == nil {
			t.Fatal("segredo errado aceito")
		}
	}
	if n := store.calls.Load(); n != 2 {
		t.Errorf("consultas ao banco = %d, quero 2 (positiva e negativa em cache)", n)
	}

	// Revogada: some da visão. Vale quando o cache expira (no máximo 60 s, #59).
	delete(store.keys, "abcdefgh")
	if err := auth(validKey); err != nil {
		t.Error("cache expirou antes do TTL")
	}
	now = now.Add(time.Minute + time.Second)
	if auth(validKey) == nil {
		t.Error("chave revogada ainda aceita depois do TTL")
	}
}

func TestStoreErrorIsNotCached(t *testing.T) {
	store := newStore()
	store.err = errors.New("banco fora do ar")
	a := newAuthenticator(store, time.Minute, zap.NewNop())
	if _, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey)); err == nil {
		t.Fatal("aceito com o banco fora do ar")
	}
	store.err = nil
	if _, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey)); err != nil {
		t.Fatalf("erro do banco ficou em cache: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := map[string]struct {
		cfg Config
		ok  bool
	}{
		"padrão com banco":   {Config{DatabaseURL: "postgres://x", CacheTTL: time.Minute}, true},
		"sem cache":          {Config{DatabaseURL: "postgres://x"}, true},
		"sem banco":          {Config{CacheTTL: time.Minute}, false},
		"cache acima de 60s": {Config{DatabaseURL: "postgres://x", CacheTTL: 2 * time.Minute}, false},
	}
	for name, c := range cases {
		if err := c.cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: Validate = %v", name, err)
		}
	}
}

func clientAuth(ctx context.Context) client.AuthData {
	if a := client.FromContext(ctx).Auth; a != nil {
		return a
	}
	return authData{}
}
