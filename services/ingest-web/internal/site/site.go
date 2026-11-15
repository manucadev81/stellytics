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
	"sync/atomic"
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

// queryTimeout limita a consulta compartilhada entre as requisições que
// esperam o mesmo site. Ela não herda o cancelamento de nenhuma requisição.
const queryTimeout = 5 * time.Second

// Cached guarda as respostas de outro Store por um TTL curto, inclusive as
// negativas. Erros não ficam em cache.
//
// Com WithStale, quando a consulta falha, a última resposta conhecida do
// mesmo site ainda é usada por até stale depois de vencer (stale-if-error).
// Regras: site nunca visto continua com erro (nunca "aceita por padrão");
// resposta negativa continua negativa; passado o stale, volta o erro. A
// revogação ocorrida durante a falha só vale quando o banco volta. Há uma só
// consulta em voo por site.
type Cached struct {
	next  Store
	ttl   time.Duration
	stale time.Duration
	now   func() time.Time

	staleServed atomic.Uint64

	mu       sync.Mutex
	entries  map[string]entry
	inflight map[string]*call
}

type entry struct {
	active  bool
	expires time.Time
}

// call é a consulta em voo de um site; quem chega depois espera done.
type call struct {
	done   chan struct{}
	active bool
	err    error
}

// NewCached embrulha next com um cache de ttl.
func NewCached(next Store, ttl time.Duration) *Cached {
	return &Cached{
		next: next, ttl: ttl, now: time.Now,
		entries: make(map[string]entry), inflight: make(map[string]*call),
	}
}

// WithStale liga o stale-if-error: respostas vencidas há menos de stale podem
// ser servidas quando a consulta falha. Zero (o padrão) desliga.
func (c *Cached) WithStale(stale time.Duration) *Cached {
	c.stale = stale
	return c
}

// StaleServed conta as respostas vencidas servidas por causa de erro do banco.
func (c *Cached) StaleServed() uint64 { return c.staleServed.Load() }

// Active implementa Store.
func (c *Cached) Active(ctx context.Context, websiteID string) (bool, error) {
	now := c.now()
	c.mu.Lock()
	e, ok := c.entries[websiteID]
	if ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.active, nil
	}
	cl, running := c.inflight[websiteID]
	if !running {
		cl = &call{done: make(chan struct{})}
		c.inflight[websiteID] = cl
		go c.fetch(websiteID, cl)
	}
	c.mu.Unlock()

	select {
	case <-cl.done:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if cl.err == nil {
		return cl.active, nil
	}
	// Falha da consulta: só uma entrada conhecida e vencida há menos de stale
	// pode responder; sem entrada, o erro segue como sempre.
	if c.stale > 0 && ok && now.Before(e.expires.Add(c.stale)) {
		c.staleServed.Add(1)
		return e.active, nil
	}
	return false, cl.err
}

// fetch faz a consulta de um site e acorda quem espera. Roda numa goroutine
// própria para que o cancelamento de uma requisição não derrube as demais.
func (c *Cached) fetch(websiteID string, cl *call) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	active, err := c.next.Active(ctx, websiteID)
	cl.active, cl.err = active, err

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, websiteID)
	if err == nil {
		c.store(websiteID, entry{active: active, expires: now.Add(c.ttl)}, now)
	}
	close(cl.done)
}

// store grava a entrada respeitando o teto de maxCached. Quem chama segura mu.
func (c *Cached) store(id string, e entry, now time.Time) {
	if _, exists := c.entries[id]; !exists && len(c.entries) >= maxCached {
		// Apaga o que já passou da janela útil (TTL mais stale).
		for k, old := range c.entries {
			if !now.Before(old.expires.Add(c.stale)) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxCached {
			clear(c.entries)
		}
	}
	c.entries[id] = e
}
