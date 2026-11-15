package ingestkeyauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
		"sem header":              {},
		"sem Bearer":              header("Authorization", validKey),
		"outro esquema":           header("Authorization", "Basic "+validKey),
		"formato errado":          header("Authorization", "Bearer ntf_ABCDEFGH_x"),
		"segredo errado":          header("Authorization", "Bearer "+wrongSecret),
		"prefixo desconhecido":    header("Authorization", "Bearer ntf_zzzzzzzz_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"),
		"chave de leitura do SCF": header("Authorization", "Bearer nrk_abcdefgh_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"),
		"dois valores":            {"Authorization": {"Bearer " + validKey, "Bearer " + validKey}},
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

// touchStore soma o fakeStore com o registro de último uso.
type touchStore struct {
	*fakeStore
	touched chan string
	err     error
}

func (s *touchStore) Touch(_ context.Context, prefix string) error {
	s.touched <- prefix
	return s.err
}

func waitTouch(t *testing.T, s *touchStore, wait ...time.Duration) bool {
	t.Helper()
	d := 2 * time.Second
	if len(wait) > 0 {
		d = wait[0]
	}
	select {
	case <-s.touched:
		return true
	case <-time.After(d):
		return false
	}
}

func TestTouchOncePerDay(t *testing.T) {
	s := &touchStore{fakeStore: newStore(), touched: make(chan string, 10)}
	a := newAuthenticator(s, time.Nanosecond, zap.NewNop())
	now := time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	auth := func() {
		t.Helper()
		if _, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey)); err != nil {
			t.Fatal(err)
		}
	}

	auth()
	if !waitTouch(t, s) {
		t.Fatal("primeira autenticação do dia não gravou")
	}
	now = now.Add(30 * time.Minute) // ainda dia 15
	auth()
	auth()
	if waitTouch(t, s, 100*time.Millisecond) {
		t.Fatal("gravou de novo no mesmo dia")
	}
	now = now.Add(time.Hour) // dia 16
	auth()
	if !waitTouch(t, s) {
		t.Fatal("não gravou no dia seguinte")
	}
}

// lockedBuffer deixa a goroutine do registro e o teste usarem o mesmo buffer.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestTouchRetriesAfterFailureAndIgnoresRejected(t *testing.T) {
	s := &touchStore{fakeStore: newStore(), touched: make(chan string, 10), err: errors.New("boom com ntf_secreto")}
	var logs lockedBuffer
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&logs), zapcore.WarnLevel)
	a := newAuthenticator(s, time.Nanosecond, zap.New(core))
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }

	// Chave recusada nunca grava.
	bad := "ntf_abcdefgh_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+bad)); err == nil {
		t.Fatal("chave errada aceita")
	}
	if waitTouch(t, s, 100*time.Millisecond) {
		t.Fatal("chave recusada gravou")
	}

	if _, err := a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey)); err != nil {
		t.Fatal(err)
	}
	if !waitTouch(t, s) {
		t.Fatal("não gravou")
	}
	time.Sleep(50 * time.Millisecond) // deixa a goroutine registrar a falha
	now = now.Add(time.Minute)
	_, _ = a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey))
	if waitTouch(t, s, 100*time.Millisecond) {
		t.Fatal("tentou de novo antes do intervalo")
	}
	now = now.Add(6 * time.Minute)
	_, _ = a.Authenticate(context.Background(), header("Authorization", "Bearer "+validKey))
	if !waitTouch(t, s) {
		t.Fatal("não tentou de novo depois do intervalo")
	}
	if strings.Contains(logs.String(), "ntf_") || strings.Contains(logs.String(), "secreto") {
		t.Fatalf("log vazou a chave ou a mensagem do erro: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "*errors.errorString") {
		t.Fatalf("log sem o nome do erro: %s", logs.String())
	}
}
