#!/usr/bin/env bash
# Teste da replicação do Postgres e da ingestão replicada (#52, ADR 0009).
# Os gateways do teste leem do primário e, depois, do banco promovido (pelo
# apelido pg-primary); a ingest_site da RÉPLICA é lida por psql como notify_ingest.
# Uso, da raiz do repositório, com a imagem do gateway já construída
# (docker compose build ingest-web; INGEST_WEB_IMAGE troca a imagem):
#   infra/compose/test/ha-replication-test.sh
#
# Cenário (tudo na máquina local, em redes do Docker; a rede privada da Hetzner,
# o balanceador e o IP flutuante NÃO são testados aqui):
#  A. primário com prod.yaml + prod-ha-primary.yaml e WAL-G num S3 local; os dados
#     chegam à réplica (prod-standby.yaml); a réplica é somente leitura; o papel
#     replicator só replica; dois gateways (prod-ingest.yaml) leem a ingest_site
#     da réplica e validam a prova de JS um do outro (mesmo PROOF_SECRET);
#  B. o primário cai, a réplica é promovida e aceita escrita;
#  C. a réplica promovida passa a rodar como o `postgres` do prod.yaml (mesmo
#     diretório de dados), arquiva WAL no S3 e os gateways leem dela;
#  D. o antigo primário volta como réplica da nova (sem dados, pg_basebackup) e
#     fica idêntico.
set -euo pipefail

cd "$(dirname "$0")/.."
here=$(pwd)
root=$(cd ../.. && pwd)
prim=notify52b-prim
net="${prim}_internal"
work=$(mktemp -d "${TMPDIR:-/tmp}/notify-ha.XXXXXX")
rand() { openssl rand -hex "${1:-24}"; }
t0=$(date +%s)
stamp() { echo "[$(( $(date +%s) - t0 ))s] $*"; }
fail() { echo "FALHOU: $*" >&2; exit 1; }
ok() { stamp "ok: $*"; }

POSTGRES_PASSWORD=$(rand); NOTIFY_DB_PASSWORD=$(rand); NOTIFY_INGEST_DB_PASSWORD=$(rand)
REPL_PW=$(rand); PROOF_SECRET=$(rand); UMAMI_PW=$(rand); COLLECTOR_PW=$(rand)
S3_KEY=$(rand 10); S3_SECRET=$(rand); LIBSODIUM=$(rand 32)
INGEST_IMG=${INGEST_WEB_IMAGE:-notify-ingest-web:dev}
PG_IMG=notify-postgres-walg:17.11-walg3.0.9
PGPORT_PRIM=${TEST_PRIM_PORT:-15452}      # admin do primário (127.0.0.1)
PGPORT_HA=${TEST_PRIM_HA_PORT:-15453}     # "IP privado" do primário (127.0.0.1)
PGPORT_STBY=${TEST_STBY_PORT:-15454}
PGPORT_OLD=${TEST_OLD_PORT:-15455}
ING1=${TEST_ING1_PORT:-18081}; ING2=${TEST_ING2_PORT:-18082}

# Sem a imagem local, o Compose a constrói (precisa do BuildKit, por causa do
# TARGETARCH do Dockerfile); com ela, reaproveita.
BUILD=--build; docker image inspect "$PG_IMG" >/dev/null 2>&1 && BUILD=--no-build

mkdir -p "$work/prim" "$work/stby" "$work/umami"
: > "$work/umami/events.log"

write_env() { # $1 arquivo, $2 diretório de dados
  cat > "$1" <<EOT
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
UMAMI_DB_PASSWORD=$UMAMI_PW
NOTIFY_DB_PASSWORD=$NOTIFY_DB_PASSWORD
NOTIFY_INGEST_DB_PASSWORD=$NOTIFY_INGEST_DB_PASSWORD
NOTIFY_COLLECTOR_DB_PASSWORD=$COLLECTOR_PW
UMAMI_APP_SECRET=$(rand)
PORTAL_AUTH_SECRET=$(rand)
INGEST_PROOF_SECRET=$PROOF_SECRET
WALG_S3_PREFIX=s3://notify-backup/ha
WALG_S3_ENDPOINT=http://s3:8333
WALG_S3_REGION=us-east-1
WALG_S3_ACCESS_KEY=$S3_KEY
WALG_S3_SECRET_KEY=$S3_SECRET
WALG_LIBSODIUM_KEY=$LIBSODIUM
PORTAL_IMAGE=notify-portal:dev
INGEST_WEB_IMAGE=$INGEST_IMG
PORTAL_HOST=portal.localhost
INGEST_HOST=ingest.localhost
ACME_EMAIL=ops@example.org
SMTP_URL=smtp://127.0.0.1:25
MAIL_FROM=notify <no-reply@example.org>
NOTIFY_DATA_DIR=$2
POSTGRES_ADMIN_PORT=$PGPORT_PRIM
REPLICATION_DB_PASSWORD=$REPL_PW
REPLICATION_ALLOWED_CIDR=172.31.52.0/24,fd31:6e6f:5200::/64
HA_PRIVATE_IP=127.0.0.1
HA_POSTGRES_PORT=$PGPORT_HA
HA_UMAMI_PORT=${TEST_UMAMI_PORT:-13052}
PRIMARY_HOST=pg-primary
TEST_NET=$net
DB_HOST=pg-primary
UMAMI_HOST=notify52b-fake-umami
EOT
}
write_env "$work/env.prim" "$work/prim"
write_env "$work/env.newprim" "$work/stby"          # mesmo projeto, outro diretório de dados
write_env "$work/env.stby" "$work/stby"
write_env "$work/env.old" "$work/prim"
mkdir -p "$work/ing"
write_env "$work/env.ing" "$work/ing"

P=(-f prod.yaml -f prod-ha-primary.yaml -f test/prod-test.yaml -f test/ha-test.yaml)
prim_c() { docker compose -p "$prim" --env-file "$1" "${P[@]}" "${@:2}"; }
stby_c() { TEST_PG_PORT=$PGPORT_STBY docker compose -p notify52b-stby --env-file "$work/env.stby" -f prod-standby.yaml -f test/ha-join.yaml "$@"; }
old_c() { TEST_PG_PORT=$PGPORT_OLD docker compose -p notify52b-old --env-file "$work/env.old" -f prod-standby.yaml -f test/ha-join.yaml "$@"; }
ing_c() { TEST_INGEST_PORT=$2 docker compose -p "notify52b-$1" --env-file "$work/env.ing" -f prod-ingest.yaml -f test/ha-join-ingest.yaml "${@:3}"; }

# psql de um cliente na rede do teste (a origem cai na faixa do pg_hba).
cpsql() { # senha, host, usuário, banco, SQL...
  local pw=$1 host=$2 user=$3 db=$4; shift 4
  docker run --rm --network "$net" -e PGPASSWORD="$pw" -e PGCONNECT_TIMEOUT=10 "$PG_IMG" psql -h "$host" -U "$user" -d "$db" -Atc "$@"
}
psql_at() { # $1 contêiner, demais: SQL (como postgres, via socket)
  docker exec "$1" psql -U postgres -d notify -Atc "$2"
}
cid() { "$@" ps -q postgres; }

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    for c in "prim_c $work/env.prim" "stby_c" "old_c"; do $c logs --tail 40 postgres >&2 2>/dev/null || true; done
  fi
  for n in ing1 ing2; do ing_c $n 0 down -v >/dev/null 2>&1 || true; done
  old_c down -v >/dev/null 2>&1 || true
  stby_c down -v >/dev/null 2>&1 || true
  docker rm -f notify52b-fake-umami >/dev/null 2>&1 || true
  prim_c "$work/env.prim" down -v --remove-orphans >/dev/null 2>&1 || true
  docker run --rm -v "$work:/w" alpine:3.22 rm -rf /w/prim /w/stby /w/ing >/dev/null 2>&1 || true
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

wait_for() { # descrição, tentativas, comando...
  local what=$1 n=$2; shift 2
  for _ in $(seq "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  fail "tempo esgotado: $what"
}

# ---------------------------------------------------------------- A. réplica
# HA_PRIVATE_IP ou REPLICATION_DB_PASSWORD vazios abortam o Compose (:?).
for var in HA_PRIVATE_IP REPLICATION_DB_PASSWORD; do
  if err=$(env "$var=" docker compose -p "$prim" --env-file "$work/env.prim" "${P[@]}" config 2>&1); then fail "$var vazio não abortou o compose"; fi
  echo "$err" | grep -q "required variable $var is missing a value" || fail "$var: erro inesperado: $err"
done
ok "HA_PRIVATE_IP e REPLICATION_DB_PASSWORD vazios abortam o Compose"

stamp "A. sobe S3 local e o primário"
prim_c "$work/env.prim" up -d s3 --wait
prim_c "$work/env.prim" exec -T s3 sh -c 'echo "s3.bucket.create -name notify-backup" | weed shell -master=localhost:9333' >/dev/null
prim_c "$work/env.prim" up -d postgres --wait
PRIM=$(cid prim_c "$work/env.prim")

for f in "$root"/apps/portal/drizzle/*.sql; do
  docker exec -i "$PRIM" psql -U notify -d notify -v ON_ERROR_STOP=1 -q < "$f" >/dev/null || fail "migração $f"
done
ok "migrações do portal aplicadas ($(ls "$root"/apps/portal/drizzle/*.sql | wc -l) arquivos)"

# O init rodou 03-replication.sh? (volume novo)
[ "$(psql_at "$PRIM" "SELECT count(*) FROM pg_roles WHERE rolname='replicator' AND NOT rolsuper AND rolreplication")" = 1 ] || fail "papel replicator"
hba_rules() { docker exec "$1" grep -nE '^host (notify notify_ingest|replication replicator|all all [^ ]+ reject)|^host all all all ' /var/lib/postgresql/data/pg_hba.conf; }
[ "$(hba_rules "$PRIM" | grep -c -E 'notify_ingest|replicator|reject')" = 6 ] || fail "esperava 3 regras por faixa (2 faixas) no pg_hba"
# Ordem: as regras da faixa vêm ANTES da regra genérica (primeira que casa).
gen=$(hba_rules "$PRIM" | grep 'host all all all ' | cut -d: -f1)
[ "$(hba_rules "$PRIM" | grep -E 'notify_ingest|replicator|reject' | cut -d: -f1 | sort -n | tail -1)" -lt "$gen" ] || fail "regras da faixa depois da regra genérica"
[ "$(psql_at "$PRIM" "SHOW max_slot_wal_keep_size")" = 10GB ] || fail "max_slot_wal_keep_size"
ok "primário: papel replicator, pg_hba (notify_ingest, replicator e reject por faixa, antes da regra genérica) e max_slot_wal_keep_size=10GB"
# O primário não guarda WAL para uma réplica que ainda não existe.
[ "$(psql_at "$PRIM" "SELECT count(*) FROM pg_replication_slots")" = 0 ] || fail "slot criado cedo demais"

# Idempotência do script em volume existente.
hba_before=$(docker exec "$PRIM" cat /var/lib/postgresql/data/pg_hba.conf | md5sum)
out=$(prim_c "$work/env.prim" exec -T postgres sh < "$root/infra/compose/postgres/init/03-replication.sh")
echo "$out" | grep -q '03-replication: changed' && fail "reaplicar disse que mudou algo"
[ "$(docker exec "$PRIM" cat /var/lib/postgresql/data/pg_hba.conf | md5sum)" = "$hba_before" ] || fail "03 mudou o pg_hba ao reaplicar"
[ "$(hba_rules "$PRIM" | grep -c -E 'notify_ingest|replicator|reject')" = 6 ] || fail "03 duplicou linhas do pg_hba"
ok "03-replication.sh reaplicado: pg_hba idêntico byte a byte, sem duplicar nada, sem reportar mudança"

# Papéis: replicator não entra em banco de aplicação; notify_ingest não replica.
if cpsql "$REPL_PW" pg-primary replicator notify 'select 1' >/dev/null 2>&1; then fail "replicator abriu o banco notify"; fi
if cpsql "$NOTIFY_INGEST_DB_PASSWORD" pg-primary notify_ingest "dbname=postgres replication=true" 'IDENTIFY_SYSTEM' >/dev/null 2>&1; then fail "notify_ingest abriu replicação"; fi
ok "replicator não abre o banco notify; notify_ingest não abre conexão de replicação"
# pg_hba da faixa: de um cliente na rede, tudo fora notify_ingest e replicator é rejeitado.
for pair in "postgres:$POSTGRES_PASSWORD:postgres" "umami:$UMAMI_PW:umami" "notify:$NOTIFY_DB_PASSWORD:notify" "notify_collector:$COLLECTOR_PW:notify"; do
  IFS=: read -r u pw db <<<"$pair"
  if err=$(cpsql "$pw" pg-primary "$u" "$db" 'select 1' 2>&1); then fail "$u abriu $db pela rede da faixa"; fi
  echo "$err" | grep -q 'pg_hba.conf rejects connection' || fail "$u: erro inesperado (esperava rejeição do pg_hba): $err"
done
[ "$(cpsql "$NOTIFY_INGEST_DB_PASSWORD" pg-primary notify_ingest notify 'select 1')" = 1 ] || fail "notify_ingest não conectou"
[ -n "$(cpsql "$REPL_PW" pg-primary replicator "dbname=postgres replication=true" 'IDENTIFY_SYSTEM')" ] || fail "replicator não replicou"
ok "pela faixa da rede: postgres, umami, notify e notify_collector rejeitados pelo pg_hba; notify_ingest e replicator funcionam"

stamp "dados no primário"
docker exec -i "$PRIM" psql -U postgres -d notify -v ON_ERROR_STOP=1 -q <<'SQL'
CREATE TABLE ha_probe (id int PRIMARY KEY, v text NOT NULL);
INSERT INTO ha_probe SELECT g, md5(g::text) FROM generate_series(1, 50000) g;
SQL
checksum() { psql_at "$1" "SELECT count(*) || ' ' || md5(string_agg(id || v, ',' ORDER BY id)) FROM ha_probe"; }
SUM1=$(checksum "$PRIM"); echo "primário: $SUM1"

# Uma tenant e dois sites para o gateway.
SITE_A=$(cat /proc/sys/kernel/random/uuid); SITE_B=$(cat /proc/sys/kernel/random/uuid); SITE_C=$(cat /proc/sys/kernel/random/uuid)
docker exec -i "$PRIM" psql -U notify -d notify -v ON_ERROR_STOP=1 -q <<SQL
INSERT INTO tenant (name, slug, status) VALUES ('ha-active', 'ha-active', 'active'), ('ha-removed', 'ha-removed', 'removed');
INSERT INTO site (tenant_id, name, domain, umami_website_id) SELECT id, 'a', 'a.example', '$SITE_A' FROM tenant WHERE slug = 'ha-active';
INSERT INTO site (tenant_id, name, domain, umami_website_id) SELECT id, 'b', 'b.example', '$SITE_B' FROM tenant WHERE slug = 'ha-removed';
SQL

stamp "sobe a réplica (pg_basebackup pela rede)"
tb=$(date +%s)
stby_c up -d $BUILD --wait --wait-timeout 300
STBY=$(stby_c ps -q postgres)
stamp "réplica pronta em $(( $(date +%s) - tb ))s"

[ "$(psql_at "$STBY" "SELECT pg_is_in_recovery()")" = t ] || fail "réplica não está em recuperação"
lag_ok() { [ "$(psql_at "$PRIM" "SELECT count(*) FROM pg_stat_replication WHERE state='streaming' AND application_name <> ''")" = 1 ]; }
wait_for "streaming" 30 lag_ok
SUM2=$(checksum "$STBY"); echo "réplica:  $SUM2"
[ "$SUM1" = "$SUM2" ] || fail "contagem/checksum diferem"
ok "50000 linhas no primário e na réplica, mesmo checksum ($SUM2)"

# Linha nova: atraso medido.
docker exec "$PRIM" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (50001, 'depois')"
t1=$(date +%s%N)
for _ in $(seq 100); do [ "$(psql_at "$STBY" "SELECT count(*) FROM ha_probe WHERE id=50001")" = 1 ] && break; sleep 0.05; done
echo "atraso até a linha nova aparecer na réplica: $(( ($(date +%s%N) - t1) / 1000000 )) ms (inclui o docker exec)"
[ "$(psql_at "$STBY" "SELECT count(*) FROM ha_probe WHERE id=50001")" = 1 ] || fail "linha nova não chegou"
echo "slot: $(psql_at "$PRIM" "SELECT slot_name || ' active=' || active FROM pg_replication_slots")"
echo "pg_stat_replication: $(psql_at "$PRIM" "SELECT state || ' ' || sync_state FROM pg_stat_replication")"

# Somente leitura.
if docker exec "$STBY" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (99999, 'x')" 2>"$work/ro.err"; then fail "réplica aceitou escrita"; fi
grep -q 'read-only transaction' "$work/ro.err" || fail "erro inesperado na escrita: $(cat "$work/ro.err")"
ok "a réplica recusa escrita: $(tr -d '\n' < "$work/ro.err")"

# WAL-G convive: backup base no primário; a réplica não arquiva.
prim_c "$work/env.prim" exec -T postgres notify-backup >"$work/backup.out" 2>&1 || { cat "$work/backup.out"; fail "notify-backup no primário"; }
[ "$(prim_c "$work/env.prim" exec -T postgres wal-g backup-list 2>/dev/null | grep -c '^base_')" -ge 1 ] || fail "backup base não apareceu"
ok "notify-backup funciona no primário com a replicação ligada"
[ "$(psql_at "$STBY" "SELECT archived_count FROM pg_stat_archiver")" = 0 ] || fail "a réplica arquivou WAL"
ok "a réplica não arquiva WAL (archived_count=0), então não disputa o prefixo do WAL-G"

# Gateways: fake Umami + duas réplicas de ingestão (prod-ingest.yaml).
stamp "gateways de ingestão"
docker run -d --name notify52b-fake-umami --network "$net" \
  -v "$here/test/fake-umami.py:/fake-umami.py:ro" -v "$work/umami:/out" \
  python:3.13-slim python /fake-umami.py /out/events.log >/dev/null
# ing1 sobe também o Caddy (prod-ingest.yaml) para conferir as rotas; ing2 só o gateway.
ing_c ing1 "$ING1" up -d --wait --wait-timeout 120
ing_c ing2 "$ING2" up -d ingest-web --wait --wait-timeout 120
chttp() { curl -sk -o /dev/null -w '%{http_code}' --resolve "ingest.localhost:${TEST_CADDY_HTTPS:-18443}:127.0.0.1" "$@"; }
cu="https://ingest.localhost:${TEST_CADDY_HTTPS:-18443}"
wait_for "Caddy da ingestão" 60 test "$(chttp "$cu/healthz")" = 200
[ "$(chttp "$cu/c?s=$(cat /proc/sys/kernel/random/uuid)")" = 200 ] || fail "Caddy: /c"
[ "$(chttp -X POST -H 'Content-Type: text/plain' -d '{}' "$cu/e")" = 400 ] || fail "Caddy: POST /e deveria chegar ao gateway (400 por corpo inválido)"
for path in / /api/send /api/heartbeat /api/websites /api/record /sign-in /login; do
  [ "$(chttp "$cu$path")" = 404 ] || fail "Caddy da ingestão deveria dar 404 em $path"
done
ok "Caddy de prod-ingest.yaml: /healthz, /c e /e passam; /, /api/send, /api/heartbeat, /api/websites, /api/record e rotas do portal dão 404"

# O gateway lê da RÉPLICA: DB_HOST aponta para o apelido pg-primary, que por ora
# é o primário; para ler da réplica, o teste troca o env dos gateways.
# (a leitura de ingest_site pela réplica é conferida logo abaixo com psql)
STBY_HOST=notify52b-stby-postgres-1
if cpsql "$NOTIFY_INGEST_DB_PASSWORD" "$STBY_HOST" notify_ingest notify "SELECT umami_website_id || ' ' || accepts_ingest FROM ingest_site ORDER BY accepts_ingest DESC" >"$work/ing.out" 2>&1; then :; else cat "$work/ing.out"; fail "notify_ingest não leu ingest_site na réplica"; fi
echo "ingest_site lida da réplica como notify_ingest:"; sed 's/^/  /' "$work/ing.out"
grep -q "$SITE_A true" "$work/ing.out" && grep -q "$SITE_B false" "$work/ing.out" || fail "conteúdo de ingest_site na réplica"
if cpsql "$NOTIFY_INGEST_DB_PASSWORD" "$STBY_HOST" notify_ingest notify "SELECT count(*) FROM tenant" >/dev/null 2>&1; then fail "notify_ingest leu a tabela tenant"; fi
ok "notify_ingest lê só a visão ingest_site (a tabela tenant é negada)"

send_event() { # porta de origem do desafio, porta de destino, site
  python3 - "$1" "$2" "$3" <<'PY'
import hashlib, json, sys, urllib.request
src, dst, site = sys.argv[1:]
ch = json.load(urllib.request.urlopen(f"http://127.0.0.1:{src}/c?s={site}"))
t, d = ch["t"], ch["d"]
n = 0
while True:
    h = hashlib.sha256(f"{t}:{n}".encode()).digest()
    bits = int.from_bytes(h, "big") >> (256 - d) if d else 0
    if bits == 0:
        break
    n += 1
body = json.dumps({"type": "event", "p": {"t": t, "n": str(n)}, "payload": {"website": site, "hostname": "a.example", "url": "/x"}})
req = urllib.request.Request(f"http://127.0.0.1:{dst}/e", data=body.encode(), headers={"Content-Type": "text/plain"})
print(urllib.request.urlopen(req).status)
PY
}
events() { grep -c "$1" "$work/umami/events.log" || true; }
check_events() { # etiqueta; confere A aceito (pelos dois gateways, prova cruzada) e B/desconhecido descartados
  local before; before=$(events "$SITE_A")
  [ "$(send_event "$ING1" "$ING1" "$SITE_A")" = 204 ] || fail "$1: POST /e em ing1"
  [ "$(send_event "$ING1" "$ING2" "$SITE_A")" = 204 ] || fail "$1: POST /e em ing2 com prova do ing1"
  send_event "$ING2" "$ING1" "$SITE_B" >/dev/null
  send_event "$ING1" "$ING2" "$(cat /proc/sys/kernel/random/uuid)" >/dev/null
  sleep 1
  [ "$(( $(events "$SITE_A") - before ))" = 2 ] || fail "$1: esperava 2 eventos do site A no Umami falso, vieram $(( $(events "$SITE_A") - before ))"
  [ "$(events "$SITE_B")" = 0 ] || fail "$1: evento do tenant removido chegou ao Umami"
  ok "$1: 2 eventos aceitos (prova emitida por uma réplica e validada pela outra), tenant removido e site desconhecido descartados"
}
check_events "gateways -> primário"

# --------------------------------------------------------------- B. failover
stamp "B. o primário cai; promoção da réplica"
docker exec "$PRIM" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (50002, 'ultima-antes-da-queda')"
wait_for "réplica recebeu a última linha" 20 bash -c "[ \"\$(docker exec $STBY psql -U postgres -d notify -Atc 'SELECT count(*) FROM ha_probe WHERE id=50002')\" = 1 ]"
prim_c "$work/env.prim" stop postgres
tp=$(date +%s%N)
[ "$(psql_at "$STBY" "SELECT pg_promote(wait => true, wait_seconds => 60)")" = t ] || fail "pg_promote"
echo "promoção (pg_promote com wait): $(( ($(date +%s%N) - tp) / 1000000 )) ms"
[ "$(psql_at "$STBY" "SELECT pg_is_in_recovery()")" = f ] || fail "continua em recuperação"
docker exec "$STBY" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (60001, 'escrito-na-promovida')"
ok "réplica promovida aceita escrita (linha 60001)"
echo "timeline (prefixo do nome do segmento WAL): $(psql_at "$STBY" "SELECT substr(pg_walfile_name(pg_current_wal_lsn()), 1, 8)")"

# ----------------------------------------- C. a promovida vira o postgres do prod.yaml
stamp "C. a réplica promovida passa a rodar como o postgres do prod.yaml (mesmo diretório de dados)"
stby_c down >/dev/null
tc=$(date +%s)
prim_c "$work/env.newprim" up -d postgres --wait --wait-timeout 120
NEW=$(cid prim_c "$work/env.newprim")
echo "novo primário pronto em $(( $(date +%s) - tc ))s"
[ "$(psql_at "$NEW" "SELECT pg_is_in_recovery()")" = f ] || fail "novo primário em recuperação"
[ "$(checksum "$NEW" | cut -d' ' -f1)" = 50003 ] || fail "contagem no novo primário: $(checksum "$NEW")"
ok "novo primário tem as 50003 linhas (50000 + 50001 + 50002 + 60001), nenhuma perdida"
docker exec "$NEW" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (60002, 'escrito-no-novo-primario')"
prim_c "$work/env.newprim" exec -T postgres sh < "$root/infra/compose/postgres/init/03-replication.sh" >/dev/null
[ "$(docker exec "$NEW" grep -c '^host replication replicator' /var/lib/postgresql/data/pg_hba.conf)" = 2 ] || fail "pg_hba do novo primário"
ok "novo primário escreve; 03-replication.sh reaplicado nele continua idempotente"

# WAL-G no novo primário: arquiva e faz backup base.
docker exec "$NEW" psql -U postgres -d postgres -qc "SELECT pg_switch_wal()" >/dev/null
arch() { [ "$(psql_at "$NEW" "SELECT archived_count FROM pg_stat_archiver")" -ge 1 ]; }
wait_for "arquivamento de WAL no novo primário" 60 arch
[ -z "$(psql_at "$NEW" "SELECT coalesce(last_failed_wal, '') FROM pg_stat_archiver")" ] || fail "arquivamento falhou: $(psql_at "$NEW" "SELECT last_failed_wal FROM pg_stat_archiver")"
prim_c "$work/env.newprim" exec -T postgres notify-backup >"$work/backup2.out" 2>&1 || { cat "$work/backup2.out"; fail "notify-backup no novo primário"; }
ok "novo primário arquiva WAL (archived_count=$(psql_at "$NEW" "SELECT archived_count FROM pg_stat_archiver")) e faz backup base; backups no S3: $(prim_c "$work/env.newprim" exec -T postgres wal-g backup-list 2>/dev/null | grep -c '^base_')"

# Gateways leem do novo primário (mesmo apelido de rede): site novo, escrito só depois da promoção.
docker exec "$NEW" psql -U notify -d notify -qc "INSERT INTO site (tenant_id, name, domain, umami_website_id) SELECT id, 'c', 'c.example', '$SITE_C' FROM tenant WHERE slug = 'ha-active'"
b=$(events "$SITE_C")
[ "$(send_event "$ING1" "$ING2" "$SITE_C")" = 204 ] && [ "$(send_event "$ING2" "$ING1" "$SITE_C")" = 204 ] || fail "POST /e depois do failover"
sleep 1
[ "$(( $(events "$SITE_C") - b ))" = 2 ] || fail "gateways não leram o site novo do banco promovido"
ok "gateways (reconectando pelo mesmo apelido) aceitam o site criado no banco já promovido"

# --------------------------------------------------- D. antigo primário volta como réplica
stamp "D. o antigo primário volta como réplica do novo (diretório de dados apagado, pg_basebackup)"
# Antes de apagar: subir a réplica sobre o diretório do antigo primário tem de falhar.
old_c up -d --no-build --wait --wait-timeout 30 >/dev/null 2>&1 && fail "réplica subiu sobre o diretório de um primário"
sleep 3
old_c logs postgres 2>&1 | grep -q 'sem standby.signal' || fail "recusa explícita não apareceu nos logs: $(old_c logs --tail 5 postgres 2>&1)"
old_c logs postgres 2>&1 | grep -q 'ready to accept connections' && fail "o Postgres abriu sobre o diretório do primário"
[ ! -e "$work/prim/standby.signal" ] || fail "o diretório do antigo primário foi alterado (standby.signal)"
old_c down >/dev/null 2>&1
ok "standby sobre o diretório de um primário antigo é recusado (erro explícito, Postgres não abre, diretório intacto)"
docker run --rm -v "$work/prim:/d" alpine:3.22 sh -c 'rm -rf /d/* /d/.[!.]* 2>/dev/null; true'
td=$(date +%s)
old_c up -d $BUILD --wait --wait-timeout 300
OLD=$(old_c ps -q postgres)
echo "antigo primário refeito como réplica em $(( $(date +%s) - td ))s"
[ "$(psql_at "$OLD" "SELECT pg_is_in_recovery()")" = t ] || fail "antigo primário não é réplica"
docker exec "$NEW" psql -U postgres -d notify -qc "INSERT INTO ha_probe VALUES (60003, 'depois-do-rejoin')"
same() { [ "$(checksum "$NEW")" = "$(checksum "$OLD")" ]; }
wait_for "réplicas iguais" 30 same
ok "antigo primário (agora réplica) idêntico ao novo: $(checksum "$OLD"); slot: $(psql_at "$NEW" "SELECT slot_name || ' active=' || active FROM pg_replication_slots")"

stamp "teste de HA concluído"
