#!/bin/sh
# Cria os usuários do banco stellytics usados pelo gateway web (stellytics_ingest) e
# pelo coletor (stellytics_collector), com o mínimo de permissão (spec do E2, D4).
# As visões que eles leem e os GRANTs vêm das migrações do portal.
#
# No Compose roda só com o volume vazio, depois de 01-databases.sh. Para um
# volume que já existe (sem apagar os dados):
#   docker compose exec -T postgres sh < infra/compose/postgres/init/02-ingest-roles.sh
# O CI usa o mesmo script contra o Postgres de serviço (PGHOST etc.).
# Idempotente: se o papel já existe, só atualiza a senha.
set -eu

psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER:-postgres}" --dbname postgres \
  -v ingest_pw="${STELLYTICS_INGEST_DB_PASSWORD:-stellytics_ingest}" \
  -v collector_pw="${STELLYTICS_COLLECTOR_DB_PASSWORD:-stellytics_collector}" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', r)
FROM unnest(ARRAY['stellytics_ingest', 'stellytics_collector']) AS r
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r)
\gexec
ALTER ROLE stellytics_ingest LOGIN PASSWORD :'ingest_pw';
ALTER ROLE stellytics_collector LOGIN PASSWORD :'collector_pw';
GRANT CONNECT ON DATABASE stellytics TO stellytics_ingest, stellytics_collector;
-- Deixa o portal assumir esses papéis (SET ROLE) nos testes de permissão, sem
-- herdar nada deles. O portal já é dono das tabelas, então não ganha acesso novo.
GRANT stellytics_ingest, stellytics_collector TO stellytics WITH INHERIT FALSE, SET TRUE;
SQL
