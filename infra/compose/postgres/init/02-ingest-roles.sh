#!/bin/sh
# Cria os usuários do banco notify usados pelo gateway web (notify_ingest) e
# pelo coletor (notify_collector), com o mínimo de permissão (spec do E2, D4).
# As visões que eles leem e os GRANTs vêm das migrações do portal.
#
# No Compose roda só com o volume vazio, depois de 01-databases.sh. Para um
# volume que já existe (sem apagar os dados):
#   docker compose exec -T postgres sh < infra/compose/postgres/init/02-ingest-roles.sh
# O CI usa o mesmo script contra o Postgres de serviço (PGHOST etc.).
# Idempotente: se o papel já existe, só atualiza a senha.
set -eu

psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER:-postgres}" --dbname postgres \
  -v ingest_pw="${NOTIFY_INGEST_DB_PASSWORD:-notify_ingest}" \
  -v collector_pw="${NOTIFY_COLLECTOR_DB_PASSWORD:-notify_collector}" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', r)
FROM unnest(ARRAY['notify_ingest', 'notify_collector']) AS r
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r)
\gexec
ALTER ROLE notify_ingest LOGIN PASSWORD :'ingest_pw';
ALTER ROLE notify_collector LOGIN PASSWORD :'collector_pw';
GRANT CONNECT ON DATABASE notify TO notify_ingest, notify_collector;
-- Deixa o portal assumir esses papéis (SET ROLE) nos testes de permissão, sem
-- herdar nada deles. O portal já é dono das tabelas, então não ganha acesso novo.
GRANT notify_ingest, notify_collector TO notify WITH INHERIT FALSE, SET TRUE;
SQL
