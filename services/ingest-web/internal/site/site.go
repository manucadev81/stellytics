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

// maxCached limita cada um dos dois mapas do cache.
const maxCached = 10_000

// queryTimeout limita a consulta compartilhada entre as requisições que
// esperam o mesmo site. Ela não herda o cancelamento de nenhuma requisição.
const queryTimeout = 5 * time.Second

// errBackoff é por quanto tempo, depois de uma falha, o stale-if-error não
// consulta o banco de novo e responde na hora (circuit breaker simples).
const errBackoff = 2 * time.Second

// Cached guarda as respostas de outro Store por um TTL curto. Erros não ficam
// em cache como resposta.
//
// Os sites aceitos ficam em `entries` e os recusados ou desconhecidos em
// `neg`, cada um com seu teto. Assim, quem testa IDs inventados só enche o
// mapa negativo e nunca expulsa um site aceito.
//
// Com WithStale, quando a consulta falha, a última resposta ACEITA do mesmo
// site ainda vale por até stale depois de vencer (stale-if-error). Site nunca
// visto, desconhecido ou recusado não ganha stale: continua com erro. Passado
// o stale, volta o erro. A revogação ocorrida durante a falha só vale quando o
// banco volta. Há uma só consulta em voo por site. Depois de uma falha, um
// circuit breaker evita novas consultas por errBackoff: a resposta vencida
// sai na hora e uma sonda em segundo plano testa o banco.
type Cached struct {
	next    Store
	ttl     time.Duration
	stale   time.Duration
	timeout time.Duration
	backoff time.Duration
	now     func() time.Time

	staleServed atomic.Uint64

	mu       sync.Mutex
	entries  map[string]entry     // aceitos
	neg      map[string]time.Time // recusados ou desconhecidos: validade
	inflight map[string]*call

	// circuit breaker (só com stale ligado)
	failed    bool
	failUntil time.Time
	lastErr   error
	probing   bool
}

type entry struct {
	expires time.Time
}

// call é a consulta em voo de um site; quem chega depois espera done.
type call struct {
	done    chan struct{}
	active  bool
	err     error
	waiters int // protegido por Cached.mu
}

// NewCached embrulha next com um cache de ttl.
func NewCached(next Store, ttl time.Duration) *Cached {
	return &Cached{
		next: next, ttl: ttl, now: time.Now,
		timeout: queryTimeout, backoff: errBackoff,
		entries: make(map[string]entry), neg: make(map[string]time.Time),
		inflight: make(map[string]*call),
	}
}

// WithStale liga o stale-if-error: respostas aceitas vencidas há menos de
// stale podem ser servidas quando a consulta falha. Zero (o padrão) desliga.
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
	e, known := c.entries[websiteID]
	if known && now.Before(e.expires) {
		c.mu.Unlock()
		return true, nil
	}
	if until, ok := c.neg[websiteID]; ok && now.Before(until) {
		c.mu.Unlock()
		return false, nil
	}

	// Circuito aberto: não espera o banco. Serve a resposta aceita vencida, se
	// couber no stale, ou o último erro; quando o backoff acaba, uma sonda em
	// segundo plano testa o banco.
	if c.stale > 0 && c.failed {
		if !now.Before(c.failUntil) && !c.probing && c.inflight[websiteID] == nil {
			c.probing = true
			cl := &call{done: make(chan struct{})}
			c.inflight[websiteID] = cl
			go c.fetch(websiteID, cl, true)
		}
		err := c.lastErr
		c.mu.Unlock()
		if known && now.Before(e.expires.Add(c.stale)) {
			c.staleServed.Add(1)
			return true, nil
		}
		return false, err
	}

	cl, running := c.inflight[websiteID]
	if !running {
		cl = &call{done: make(chan struct{})}
		c.inflight[websiteID] = cl
		go c.fetch(websiteID, cl, false)
	}
	cl.waiters++
	c.mu.Unlock()

	select {
	case <-cl.done:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if cl.err == nil {
		return cl.active, nil
	}
	// Falha da consulta: só uma entrada aceita e vencida há menos de stale pode
	// responder. A hora é lida de novo: a espera pode ter durado até o timeout.
	if c.stale > 0 && known && c.now().Before(e.expires.Add(c.stale)) {
		c.staleServed.Add(1)
		return true, nil
	}
	return false, cl.err
}

// fetch faz a consulta de um site e acorda quem espera. Roda numa goroutine
// própria para que o cancelamento de uma requisição não derrube as demais.
func (c *Cached) fetch(websiteID string, cl *call, probe bool) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	active, err := c.next.Active(ctx, websiteID)
	cl.active, cl.err = active, err

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, websiteID)
	if probe {
		c.probing = false
	}
	if err != nil {
		if c.stale > 0 {
			c.failed, c.failUntil, c.lastErr = true, now.Add(c.backoff), err
		}
	} else {
		c.failed = false
		c.lastErr = nil
		c.store(websiteID, active, now)
	}
	close(cl.done)
}

// store grava a resposta no mapa certo, respeitando o teto de cada um. Uma
// resposta nova de um lado apaga o registro do outro, para que um site que
// passou a ser recusado não volte pelo stale. Quem chama segura mu.
func (c *Cached) store(id string, active bool, now time.Time) {
	exp := now.Add(c.ttl)
	if active {
		delete(c.neg, id)
		if _, exists := c.entries[id]; !exists && len(c.entries) >= maxCached {
			c.evictAccepted(now)
		}
		c.entries[id] = entry{expires: exp}
		return
	}
	delete(c.entries, id)
	if _, exists := c.neg[id]; !exists && len(c.neg) >= maxCached {
		for k, until := range c.neg {
			if !now.Before(until) {
				delete(c.neg, k)
			}
		}
		if len(c.neg) >= maxCached {
			clear(c.neg) // só o mapa negativo: os aceitos não são tocados
		}
	}
	c.neg[id] = exp
}

// evictAccepted abre uma vaga entre os aceitos: primeiro o que já passou da
// janela útil (TTL mais stale); se nada passou, o mais antigo.
func (c *Cached) evictAccepted(now time.Time) {
	var oldest string
	var oldestExp time.Time
	for k, e := range c.entries {
		if !now.Before(e.expires.Add(c.stale)) {
			delete(c.entries, k)
			continue
		}
		if oldest == "" || e.expires.Before(oldestExp) {
			oldest, oldestExp = k, e.expires
		}
	}
	if len(c.entries) >= maxCached && oldest != "" {
		delete(c.entries, oldest)
	}
}
