// Package site diz se um websiteId pode receber eventos.
//
// A fonte é a visão ingest_site(umami_website_id uuid, accepts_ingest boolean)
// do banco notify, criada pelas migrações do portal e lida pelo usuário
// notify_ingest, que só tem SELECT nela (spec do E2, seções 4 e 6). Site
// arquivado ou de tenant que não está ativo vem com accepts_ingest falso.
package site

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store responde se o site existe e está ativo.
type Store interface {
	Active(ctx context.Context, websiteID string) (bool, error)
}

// PG consulta a ingest_site no Postgres.
type PG struct {
	Pool *pgxpool.Pool
}

// Active implementa Store.
func (s PG) Active(ctx context.Context, websiteID string) (bool, error) {
	var accepts bool
	err := s.Pool.QueryRow(ctx,
		`SELECT accepts_ingest FROM ingest_site WHERE umami_website_id = $1`, websiteID,
	).Scan(&accepts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return accepts, nil
}

// maxCached limita o mapa do cache, que também guarda respostas negativas.
const maxCached = 10_000

// Cached guarda as respostas de outro Store por um TTL curto, inclusive as
// negativas. Erros não ficam em cache.
type Cached struct {
	next Store
	ttl  time.Duration
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	active  bool
	expires time.Time
}

// NewCached embrulha next com um cache de ttl.
func NewCached(next Store, ttl time.Duration) *Cached {
	return &Cached{next: next, ttl: ttl, now: time.Now, entries: make(map[string]entry)}
}

// Active implementa Store.
func (c *Cached) Active(ctx context.Context, websiteID string) (bool, error) {
	now := c.now()
	c.mu.Lock()
	e, ok := c.entries[websiteID]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.active, nil
	}

	active, err := c.next.Active(ctx, websiteID)
	if err != nil {
		return false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCached {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxCached {
			clear(c.entries)
		}
	}
	c.entries[websiteID] = entry{active: active, expires: now.Add(c.ttl)}
	return active, nil
}
