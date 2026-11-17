package ingestkeyauth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore lê a visão ingest_key_active (spec do E2, seção 4) com o usuário
// stellytics_collector, que só tem SELECT nela.
type pgStore struct {
	pool *pgxpool.Pool
}

func (s pgStore) Lookup(ctx context.Context, prefix string) (Key, bool, error) {
	var k Key
	err := s.pool.QueryRow(ctx,
		`SELECT key_hash, tenant_id::text, api_service_id::text FROM ingest_key_active WHERE prefix = $1`, prefix,
	).Scan(&k.Hash, &k.TenantID, &k.ServiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Key{}, false, nil
	}
	if err != nil {
		return Key{}, false, err
	}
	return k, true, nil
}

// Touch chama ingest_key_touch (migração 0008), a única escrita que o
// stellytics_collector pode fazer em ingest_key: grava o dia de hoje (UTC) em
// last_used_on, só para chave ativa e só se a data mudou.
func (s pgStore) Touch(ctx context.Context, prefix string) error {
	var updated bool
	return s.pool.QueryRow(ctx, `SELECT ingest_key_touch($1)`, prefix).Scan(&updated)
}
