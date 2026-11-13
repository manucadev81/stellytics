package site

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingStore struct {
	calls  int
	active map[string]bool
	err    error
}

func (s *countingStore) Active(_ context.Context, id string) (bool, error) {
	s.calls++
	return s.active[id], s.err
}

func TestCachedKeepsPositiveAndNegative(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	next := &countingStore{active: map[string]bool{"a": true}}
	c := NewCached(next, time.Minute)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	for range 3 {
		if ok, _ := c.Active(ctx, "a"); !ok {
			t.Fatal("a deveria estar ativo")
		}
		if ok, _ := c.Active(ctx, "b"); ok {
			t.Fatal("b não deveria estar ativo")
		}
	}
	if next.calls != 2 {
		t.Fatalf("consultas = %d, quero 2", next.calls)
	}

	// Depois do TTL, a resposta é buscada de novo.
	next.active["b"] = true
	now = now.Add(time.Minute)
	if ok, _ := c.Active(ctx, "b"); !ok {
		t.Fatal("b não foi atualizado depois do TTL")
	}
	if next.calls != 3 {
		t.Fatalf("consultas = %d, quero 3", next.calls)
	}
}

func TestCachedDoesNotKeepErrors(t *testing.T) {
	next := &countingStore{err: errors.New("banco fora")}
	c := NewCached(next, time.Minute)
	ctx := context.Background()
	for range 2 {
		if _, err := c.Active(ctx, "a"); err == nil {
			t.Fatal("erro não repassado")
		}
	}
	if next.calls != 2 {
		t.Fatalf("consultas = %d, quero 2", next.calls)
	}
}

func TestCachedIsBounded(t *testing.T) {
	c := NewCached(&countingStore{}, time.Hour)
	ctx := context.Background()
	for i := range maxCached + 10 {
		_, _ = c.Active(ctx, string(rune(i)))
	}
	if n := len(c.entries); n > maxCached {
		t.Fatalf("cache com %d entradas, limite %d", n, maxCached)
	}
}
