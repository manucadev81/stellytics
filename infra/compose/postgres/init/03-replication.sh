#!/bin/sh
# Prepara o Postgres PRIMÁRIO para ter uma réplica em streaming (#52, ADR 0009,
# degrau 2). Desligado por padrão: sem REPLICATION_DB_PASSWORD o script não faz
# nada, e o primário fica exatamente como era.
#
# O que faz, quando ligado:
# - cria (ou atualiza a senha do) papel `replicator`, só com LOGIN e
#   REPLICATION (sem acesso a nenhum banco);
# - para cada faixa de REPLICATION_ALLOWED_CIDR (a rede privada 10.50.0.0/16 do
#   OpenTofu) insere no pg_hba.conf, ANTES da regra genérica da imagem
#   (`host all all all scram-sha-256`; o pg_hba é "primeira que casa"):
#     host stellytics      stellytics_ingest <faixa> scram-sha-256   (gateways das réplicas)
#     host replication replicator    <faixa> scram-sha-256   (a réplica)
#     host all         all           <faixa> reject          (todo o resto)
#   Sem isso, publicar o 5432 no IP privado deixaria postgres, umami, stellytics e
#   stellytics_collector acessíveis pela rede privada só com senha. O tráfego do
#   próprio Compose (portal, Umami, gateway local) vem da rede 172.30.0.0/24,
#   fora destas faixas, e não é afetado;
# - limita em max_slot_wal_keep_size o WAL que o slot da réplica pode reter,
#   para uma réplica parada não encher o disco do primário (o slot em si é
#   criado pela réplica, em standby-entrypoint.sh, quando ela é montada: sem
#   réplica não há slot, e o primário não guarda WAL à toa);
# - confere que max_wal_senders e wal_level aceitam a réplica.
# Usa ALTER SYSTEM (postgresql.auto.conf) em vez de mexer na linha de comando do
# Postgres do prod.yaml, para o archive_command do WAL-G nunca ser duplicado
# aqui. Nada disso exige reiniciar o Postgres.
#
# No Compose roda com o volume vazio, depois de 02-ingest-roles.sh. Para um
# volume que já existe (idempotente, sem apagar dados):
#   docker compose -f prod.yaml -f prod-ha-primary.yaml exec -T postgres sh < infra/compose/postgres/init/03-replication.sh
set -eu

if [ -z "${REPLICATION_DB_PASSWORD:-}" ]; then
  echo "03-replication: REPLICATION_DB_PASSWORD vazia, replicação desligada."
  exit 0
fi

cidr="${REPLICATION_ALLOWED_CIDR:-10.50.0.0/16}"
keep="${REPLICATION_MAX_SLOT_WAL_KEEP:-10GB}"

# Valores que vão para o pg_hba.conf e para o SQL: só o formato esperado.
case "$cidr" in *[!0-9a-fA-F:./,]* | "") echo "REPLICATION_ALLOWED_CIDR inválido: $cidr" >&2; exit 1 ;; esac
case "$keep" in *[!0-9A-Za-z]* | "") echo "REPLICATION_MAX_SLOT_WAL_KEEP inválido: $keep" >&2; exit 1 ;; esac

psql() { command psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER:-postgres}" --dbname postgres "$@"; }

# A senha entra pelo ambiente (\getenv), não por argumento do psql (que apareceria
# na lista de processos do contêiner).
psql -v keep="$keep" <<'SQL'
\getenv repl_pw REPLICATION_DB_PASSWORD
SELECT 'CREATE ROLE replicator LOGIN REPLICATION'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'replicator')
\gexec
ALTER ROLE replicator LOGIN REPLICATION PASSWORD :'repl_pw';

SELECT format('ALTER SYSTEM SET max_slot_wal_keep_size = %L', :'keep')
\gexec

-- Falha cedo se o primário não aceita réplica (valores padrão do Postgres 17
-- servem; só quebram se alguém baixar).
DO $$
BEGIN
  IF current_setting('wal_level') NOT IN ('replica', 'logical') THEN
    RAISE EXCEPTION 'wal_level precisa ser replica ou logical';
  END IF;
  IF current_setting('max_wal_senders')::int < 3 THEN
    RAISE EXCEPTION 'max_wal_senders precisa ser pelo menos 3';
  END IF;
END
$$;
SQL

hba=$(psql -Atc 'SHOW hba_file')
# REPLICATION_ALLOWED_CIDR aceita uma lista separada por vírgulas (faixas IPv4
# e IPv6). Idempotente: tira as linhas nossas da faixa e reinsere o bloco antes
# da regra genérica (ou no fim, se ela não existir).
# Monta o bloco de todas as faixas e a lista das linhas nossas a retirar.
want=""
set --
for range in $(echo "$cidr" | tr ',' ' '); do
  l1="host stellytics stellytics_ingest $range scram-sha-256"
  l2="host replication replicator $range scram-sha-256"
  l3="host all all $range reject"
  want="$want$l1
$l2
$l3
"
  set -- "$@" -e "$l1" -e "$l2" -e "$l3"
done
tmp=$(mktemp)
grep -vxF "$@" "$hba" > "$tmp" || true
awk -v block="$want" '
  !done && $0 == "host all all all scram-sha-256" { printf "%s", block; done = 1 }
  { print }
  END { if (!done) printf "%s", block }
' "$tmp" > "$tmp.new"
changed=0
if ! cmp -s "$tmp.new" "$hba"; then
  cat "$tmp.new" > "$hba"
  changed=1
fi
rm -f "$tmp" "$tmp.new"
psql -Atc 'SELECT pg_reload_conf()' >/dev/null
[ "$changed" = 0 ] || echo "03-replication: changed"
echo "03-replication: papel replicator e pg_hba ($cidr) prontos."
