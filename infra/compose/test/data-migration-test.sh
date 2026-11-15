#!/usr/bin/env bash
# Teste da migração de dados entre operadores (#54, docs/transferencia.md).
#
# Sobe uma pilha de ORIGEM (Postgres 17 com WAL-G, Umami e portal, com as
# imagens e o Compose de produção), grava dados de verdade nos dois bancos
# (inclusive uma sessão do portal e eventos no Umami), e migra para pilhas de
# DESTINO vazias, com senhas e chave de backup diferentes, de dois jeitos:
#
#   A. lógico: infra/scripts/db-export.sh e db-import.sh (pg_dump/pg_restore);
#   B. físico: backup base e WAL do WAL-G copiados para o bucket de outro
#      operador e restaurados com notify-restore.
#
# Em cada um, prova por consulta que o destino é idêntico (db-fingerprint.sh) e
# que o sistema funciona nele: login com a senha antiga, sessão antiga válida,
# Umami com as mesmas estatísticas, chaves de ingestão aceitas pelas visões e
# audit_log ainda só de inserção. Mostra o tempo de cada passo.
#
# Uso, da raiz do repositório, com a imagem do portal já construída:
#   PORTAL_IMAGE=notify-portal:dev infra/compose/test/data-migration-test.sh
# MODE=logical ou MODE=physical roda só um dos dois. KEEP=1 não derruba nada.
# Portas no loopback: 13541-13543 (portal) e 18333-18335 (S3 local); troque com
# PORT_BASE e S3_BASE.
set -euo pipefail

cd "$(dirname "$0")/.."
repo=$(cd ../.. && pwd)
PORTAL_IMAGE="${PORTAL_IMAGE:-notify-portal:dev}"
MODE="${MODE:-both}"
PORT_BASE="${PORT_BASE:-13541}"
S3_BASE="${S3_BASE:-18333}"
work=$(mktemp -d "${TMPDIR:-/tmp}/notify-migtest.XXXXXX")
rand() { openssl rand -hex "${1:-24}"; }
ALPINE='alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f'
RCLONE='rclone/rclone:1.73.0'

fail() { echo "FALHOU: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }
t0=$(date +%s)
lap() { echo "tempo: $1 em $(( $(date +%s) - t0 ))s"; t0=$(date +%s); }

# --- uma pilha = projeto Compose + arquivo .env + pasta de dados -------------
# stack <nome> <índice> [libsodium] [auth_secret] [umami_secret]
mkenv() {
  local name=$1 idx=$2 libsodium=$3 auth=$4 umami=$5
  mkdir -p "$work/$name-data"
  cat > "$work/$name.env" <<EOT
POSTGRES_PASSWORD=$(rand)
UMAMI_DB_PASSWORD=$(rand)
NOTIFY_DB_PASSWORD=$(rand)
NOTIFY_INGEST_DB_PASSWORD=$(rand)
NOTIFY_COLLECTOR_DB_PASSWORD=$(rand)
UMAMI_APP_SECRET=$umami
PORTAL_AUTH_SECRET=$auth
INGEST_PROOF_SECRET=$(rand)
WALG_S3_PREFIX=s3://notify-backup-$name/pg
WALG_S3_ENDPOINT=http://s3:8333
WALG_S3_REGION=us-east-1
WALG_S3_ACCESS_KEY=$(rand 10)
WALG_S3_SECRET_KEY=$(rand)
WALG_LIBSODIUM_KEY=$libsodium
PORTAL_IMAGE=$PORTAL_IMAGE
INGEST_WEB_IMAGE=unused:none
PORTAL_HOST=portal.localhost
INGEST_HOST=ingest.localhost
ACME_EMAIL=ops@example.org
SMTP_URL=smtp://127.0.0.1:25
MAIL_FROM=notify <no-reply@example.org>
NOTIFY_DATA_DIR=$work/$name-data
PORTAL_PORT=$((PORT_BASE + idx))
S3_PORT=$((S3_BASE + idx))
POSTGRES_ADMIN_PORT=$((PORT_BASE + 100 + idx))
EOT
}
stacks=()
use() { P=notify54-$1; ENVF=$work/$1.env; NAME=$1; }
compose() { docker compose -p "$P" --env-file "$ENVF" -f prod.yaml -f test/prod-test.yaml -f test/migration-test.yaml "$@"; }
envval() { grep "^$1=" "$ENVF" | cut -d= -f2-; }
pgsql() { compose exec -T -u postgres postgres psql -X -At -v ON_ERROR_STOP=1 -d "$1" "${@:2}"; }
portal_url() { echo "http://127.0.0.1:$(envval PORTAL_PORT)"; }
# Conecta como um papel PELA REDE do Compose, com senha. Por dentro do contêiner
# do Postgres o socket e o 127.0.0.1 são `trust` (imagem oficial) e não provam a
# senha; por isso o teste sobe um contêiner à parte (mesma imagem, só o psql).
pg_net() { # papel senha banco sql
  compose run --rm --no-deps -T -e PGHOST=postgres -e PGUSER="$1" -e PGPASSWORD="$2" --entrypoint psql postgres -X -At -d "$3" -c "$4"
}
umami_curl() { compose exec -T umami curl -fsS "$@"; }

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    for s in "${stacks[@]}"; do use "$s"; compose logs --tail 40 postgres portal umami >&2 || true; done
  fi
  if [ "${KEEP:-0}" != 1 ]; then
    for s in "${stacks[@]}"; do use "$s"; compose down -v --remove-orphans >/dev/null 2>&1 || true; done
    docker run --rm -v "$work:/w" "$ALPINE" sh -c 'rm -rf /w/*-data /w/migracao*' >/dev/null 2>&1 || true
    rm -rf "$work"
  else
    echo "KEEP=1: pilhas e $work mantidos."
  fi
  exit "$status"
}
trap cleanup EXIT

new_stack() { # nome índice libsodium auth umami
  mkenv "$@"; stacks+=("$1"); use "$1"
  compose up -d s3 --wait
  compose exec -T s3 sh -c "echo 's3.bucket.create -name notify-backup-$1' | weed shell -master=localhost:9333" >/dev/null
  compose up -d postgres umami portal --wait
}

# 1. ORIGEM -------------------------------------------------------------------
auth_secret=$(rand 32); umami_secret=$(rand 32); libsodium_origin=$(rand 32)
echo "== origem"
new_stack origem 0 "$libsodium_origin" "$auth_secret" "$umami_secret"
lap "origem no ar (portal aplicou as migrações; Umami criou o esquema dele)"
o_url=$(portal_url)

# Dois usuários pela API real do portal (a senha vira o hash do Better Auth).
password="senha-de-teste-$(rand 4)"
signup() {
  curl -s -o /dev/null -X POST "$o_url/api/auth/sign-up/email" -H 'Content-Type: application/json' \
    -H 'Origin: https://portal.localhost' -d "{\"email\":\"$1\",\"password\":\"$password\",\"name\":\"$2\"}"
}
signup alice@example.org Alice; signup operator@example.org Operador
# Sem SMTP no teste, a confirmação do e-mail é feita direto no banco.
pgsql notify -c "update \"user\" set email_verified = true where email in ('alice@example.org','operator@example.org')"
login() { # url email -> cookie
  curl -s -D - -o /dev/null -X POST "$1/api/auth/sign-in/email" -H 'Content-Type: application/json' \
    -H 'Origin: https://portal.localhost' -d "{\"email\":\"$2\",\"password\":\"$password\"}" |
    tr -d '\r' | sed -n 's/^[Ss]et-[Cc]ookie: \(__Secure-better-auth.session_token=[^;]*\).*/\1/p' | head -1
}
cookie=$(login "$o_url" alice@example.org)
[ -n "$cookie" ] || fail "login na origem não devolveu cookie de sessão"
session_email() { # url cookie
  curl -s "$1/api/auth/get-session" -H "Cookie: $2" -H 'Origin: https://portal.localhost' |
    sed -n 's/.*"email":"\([^"]*\)".*/\1/p' | head -1
}
[ "$(session_email "$o_url" "$cookie")" = alice@example.org ] || fail "a sessão não vale na origem"
ok "usuários criados pelo portal; sessão da Alice válida na origem"

# Umami: um site e eventos de verdade, pelo mesmo /api/send que o gateway usa.
token=$(umami_curl -X POST http://localhost:3000/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"umami"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
umami_site=$(umami_curl -X POST http://localhost:3000/api/websites -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' -d '{"name":"Alpha site","domain":"alpha.example"}' |
  sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)
[ -n "$umami_site" ] || fail "o Umami não criou o site"
for i in $(seq 1 60); do
  ua="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 v$((i % 6))"
  umami_curl -o /dev/null -X POST http://localhost:3000/api/send -H 'Content-Type: application/json' \
    -H "User-Agent: $ua" -H "x-notify-client-ip: 203.0.113.$((i % 9)).0" \
    -d "{\"type\":\"event\",\"payload\":{\"website\":\"$umami_site\",\"hostname\":\"alpha.example\",\"url\":\"/p$((i % 7))\",\"title\":\"p\",\"language\":\"en-US\",\"screen\":\"1920x1080\"}}" ||
    fail "o Umami recusou o evento $i"
done
events=$(pgsql umami -c "select count(*) from website_event")
[ "$events" -ge 50 ] || fail "poucos eventos no Umami: $events"
umami_stats() { # compose já em uso; token -> JSON de /stats
  umami_curl "http://localhost:3000/api/websites/$umami_site/stats?startAt=0&endAt=4102444800000" -H "Authorization: Bearer $1"
}
stats_origin=$(umami_stats "$token")
ok "Umami com $events eventos: $stats_origin"

rk() { echo "ntf_$(openssl rand -hex 5 | xxd -r -p | base32 | tr '[:upper:]' '[:lower:]' | tr -d '=' | cut -c1-8)_$(openssl rand -base64 33 | tr '+/' '-_' | tr -d '=\n' | cut -c1-43)"; }
key_active=$(rk); key_revoked=$(rk); key_archived=$(rk); key_removed=$(rk)
pgsql notify -v alice_email=alice@example.org -v operator_email=operator@example.org -v umami_site="$umami_site" \
  -v key_active="$key_active" -v key_revoked="$key_revoked" -v key_archived="$key_archived" -v key_removed="$key_removed" \
  < test/migration-seed.sql > /dev/null
[ "$(pgsql notify -c 'select count(*) from ingest_key_active')" = 1 ] || fail "esperava 1 chave ativa na origem"
ok "dados de exemplo: $(pgsql notify -c "select (select count(*) from tenant) || ' tenants, ' || (select count(*) from api_metric_1m) || ' linhas em 1m, ' || (select count(*) from api_metric_1h) || ' em 1h, ' || (select count(*) from api_metric_1d) || ' em 1d, ' || (select count(*) from audit_log) || ' no audit_log'")"
lap "dados gravados"

# Backup base na origem antes da janela (o caminho físico parte dele).
if [ "$MODE" != logical ]; then compose exec -T -u postgres postgres notify-backup >/dev/null; lap "backup base da origem"; fi

# --- janela de manutenção: aplicação parada, só o Postgres no ar -------------
compose stop portal umami >/dev/null
lap "janela de manutenção aberta (portal e Umami parados)"
fp_origin="$work/fp-origem.txt"
COMPOSE="docker compose -p $P --env-file $ENVF -f prod.yaml -f test/prod-test.yaml -f test/migration-test.yaml" \
  "$repo/infra/scripts/db-fingerprint.sh" > "$fp_origin"
# A impressão digital precisa cobrir TODAS as tabelas (um bug já a fez parar na primeira).
for db in notify umami; do
  expected=$(pgsql "$db" -c "select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.relkind in ('r', 'p') and n.nspname not in ('pg_catalog', 'information_schema') and n.nspname not like 'pg_toast%'")
  got=$(awk -v db="$db" '$0 == "== banco " db {f = 1; next} /^== / {f = 0} f' "$fp_origin" | grep -c -E ' [0-9]+ [0-9a-f]{32}$| 0 vazia$' || true)
  [ "$expected" = "$got" ] || fail "a impressão digital cobre $got de $expected tabelas do banco $db"
  ok "impressão digital cobre as $got tabelas do banco $db"
done
lap "impressão digital da origem ($(wc -l < "$fp_origin") linhas)"
cp "$ENVF" "$work/origem.env.bak"

verify_destination() { # nome: o sistema funciona no destino
  local url token2 stats_dest
  url=$(portal_url)
  [ "$(login "$url" alice@example.org)" != "" ] || fail "[$NAME] login com a senha antiga falhou"
  ok "[$NAME] login com a senha da origem funciona"
  [ "$(session_email "$url" "$cookie")" = alice@example.org ] || fail "[$NAME] a sessão da origem não vale"
  ok "[$NAME] a sessão criada na origem continua válida"
  token2=$(umami_curl -X POST http://localhost:3000/api/auth/login -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"umami"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  stats_dest=$(umami_stats "$token2")
  [ "$stats_dest" = "$stats_origin" ] || fail "[$NAME] stats do Umami diferem: $stats_dest"
  ok "[$NAME] Umami (sem fork) abriu o banco migrado e dá as mesmas estatísticas"
  [ "$(pgsql notify -c 'select count(*) from ingest_key_active')" = 1 ] || fail "[$NAME] chaves ativas"
  [ "$(pgsql notify -c "select count(*) from ingest_key_active where prefix = '$(echo "$key_active" | cut -c5-12)' and key_hash = sha256(convert_to('$key_active', 'UTF8'))")" = 1 ] ||
    fail "[$NAME] o hash da chave ativa não bate com a chave original"
  ok "[$NAME] a chave de ingestão original continua válida (prefixo e SHA-256)"
  for stmt in "update audit_log set action = 'x'" "delete from audit_log" "truncate audit_log"; do
    if pgsql notify -c "$stmt" 2>/dev/null; then fail "[$NAME] audit_log aceitou: $stmt"; fi
  done
  pgsql notify -c "insert into audit_log (action) values ('teste.pos-migracao')" >/dev/null
  ok "[$NAME] audit_log recusa UPDATE, DELETE e TRUNCATE e aceita INSERT (gatilhos ativos)"
  [ "$(pg_net notify_ingest "$(envval NOTIFY_INGEST_DB_PASSWORD)" notify "select accepts_ingest from ingest_site where umami_website_id = '$umami_site'")" = t ] ||
    fail "[$NAME] notify_ingest não lê ingest_site com a senha nova"
  [ "$(pg_net notify_collector "$(envval NOTIFY_COLLECTOR_DB_PASSWORD)" notify "select count(*) from ingest_key_active")" = 1 ] ||
    fail "[$NAME] notify_collector não lê ingest_key_active com a senha nova"
  if pg_net notify_ingest "$(envval NOTIFY_INGEST_DB_PASSWORD)" notify "select count(*) from tenant" >/dev/null 2>&1; then
    fail "[$NAME] notify_ingest leu a tabela tenant (GRANT a mais)"
  fi
  ok "[$NAME] notify_ingest e notify_collector entram com as senhas do destino e leem as visões"
  pgsql notify -c "select api_metric_ensure_partitions(2)" >/dev/null
  [ "$(pgsql notify -c "select count(*) from api_metric_1m where route = '/future' and tableoid::regclass::text like 'api_metric_1m_p%'")" = 1 ] || fail "[$NAME] partições"
  ok "[$NAME] api_metric_ensure_partitions roda e as partições existem"
  # Escreve como o coletor, para provar o GRANT de INSERT/UPDATE em api_metric_1m.
  pg_net notify_collector "$(envval NOTIFY_COLLECTOR_DB_PASSWORD)" notify \
    "insert into api_metric_1m values (date_trunc('minute', now()), 'aaaaaaaa-0000-4000-8000-000000000001', 'cccccccc-0000-4000-8000-000000000001', '/pos', 'GET', 200, 1, 0.1, array[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0]::bigint[])" >/dev/null ||
    fail "[$NAME] notify_collector não grava em api_metric_1m"
  pgsql notify -c "delete from api_metric_1m where route = '/pos'" >/dev/null
  ok "[$NAME] notify_collector grava em api_metric_1m"
  # Desfaz as duas escritas de teste (a do audit_log não dá para apagar: fica 1 linha a mais).
}

# 2A. LÓGICO ------------------------------------------------------------------
if [ "$MODE" != physical ]; then
  echo "== A. migração lógica (pg_dump)"
  use origem
  dump="$work/migracao"
  COMPOSE="docker compose -p $P --env-file $ENVF -f prod.yaml -f test/prod-test.yaml -f test/migration-test.yaml" \
    "$repo/infra/scripts/db-export.sh" "$dump" >/dev/null
  lap "exportação (pg_dump -Fc dos dois bancos + impressão digital)"
  cfiles="-f prod.yaml -f test/prod-test.yaml -f test/migration-test.yaml"
  imp() { COMPOSE="docker compose -p $P --env-file $ENVF $cfiles" "$repo/infra/scripts/db-import.sh" "$@"; }
  dboids() { pgsql postgres -c "select string_agg(datname || ':' || oid, ',' order by datname) from pg_database where datname in ('notify','umami')"; }
  # Importar na própria ORIGEM (que tem tenants) tem de ser recusado sem --force-nonempty.
  oids=$(dboids)
  if imp --yes "$dump" >/dev/null 2>&1; then fail "db-import rodou numa pilha com tenants"; fi
  [ "$(dboids)" = "$oids" ] || fail "a recusa por destino não vazio mexeu nos bancos"
  ok "[A] db-import recusa um destino com tenants (a origem) sem --force-nonempty"

  du -h "$dump"/*.dump | sed 's/^/     /'

  # Destino: o `site.yml` já rodou (pilha completa, vazia, com portal e Umami
  # migrando os bancos vazios), com as MESMAS chaves de sessão e outras senhas.
  new_stack destino-logico 1 "$(rand 32)" "$auth_secret" "$umami_secret"
  lap "destino lógico no ar e vazio"
  [ "$(pgsql notify -c 'select count(*) from tenant')" = 0 ] || fail "destino não está vazio"
  # Proteções do import, todas ANTES de qualquer DROP: os bancos do destino
  # (OIDs) têm de continuar os mesmos depois de cada recusa.
  oids=$(dboids)
  refuse() { # descrição, pasta
    if imp --yes "$2" >/dev/null 2>&1; then fail "[A] db-import aceitou: $1"; fi
    [ "$(dboids)" = "$oids" ] || fail "[A] a recusa de '$1' mexeu nos bancos do destino"
    ok "[A] db-import recusa $1 e deixa o destino intacto"
  }
  refuse "com a aplicação rodando" "$dump"
  compose stop portal umami >/dev/null
  cp -r "$dump" "$work/migracao-adulterado" && printf x >> "$work/migracao-adulterado/notify.dump"
  refuse "um dump adulterado (sha256sums não confere)" "$work/migracao-adulterado"
  cp -r "$dump" "$work/migracao-truncado" && head -c 1000 "$dump/notify.dump" > "$work/migracao-truncado/notify.dump"
  (cd "$work/migracao-truncado" && sha256sum notify.dump umami.dump fingerprint.txt > sha256sums)
  refuse "um dump truncado com sha256sums regenerado (pg_restore -l falha)" "$work/migracao-truncado"
  cp -r "$dump" "$work/migracao-semsoma" && (cd "$work/migracao-semsoma" && sha256sum fingerprint.txt > sha256sums)
  refuse "um sha256sums que não lista os dumps" "$work/migracao-semsoma"
  imp --yes "$dump" | tail -4
  lap "importação + conferência (db-import.sh)"
  diff -u "$fp_origin" "$dump/fingerprint-destino.txt" > /dev/null || fail "impressão digital do destino difere"
  ok "[A] impressão digital idêntica (linhas e checksum por tabela, sequências, esquema, papéis, ACLs)"
  compose up -d --wait portal umami >/dev/null
  lap "aplicação do destino no ar sobre os dados migrados"
  verify_destination

fi

# 2B. FÍSICO (WAL-G) ----------------------------------------------------------
if [ "$MODE" != logical ]; then
  echo "== B. migração física (WAL-G)"
  use origem
  # Fecha o WAL da origem e confirma o arquivamento; backup base final.
  compose exec -T -u postgres postgres notify-backup >/dev/null
  wal=$(pgsql notify -c "select pg_walfile_name(pg_switch_wal())")
  for _ in $(seq 1 60); do
    archived=$(pgsql notify -c "select coalesce(last_archived_wal, '') from pg_stat_archiver")
    [[ "$archived" > "$wal" || "$archived" == "$wal" ]] && break
    sleep 1
  done
  [[ "$archived" > "$wal" || "$archived" == "$wal" ]] || fail "WAL $wal não arquivado"
  lap "backup base final e WAL $wal arquivados na origem"
  src_key=$(envval WALG_S3_ACCESS_KEY); src_secret=$(envval WALG_S3_SECRET_KEY); src_bucket=notify-backup-origem

  # Destino: bucket próprio (credenciais próprias), MESMA walg_libsodium_key, Postgres ainda não iniciado.
  mkenv destino-fisico 2 "$libsodium_origin" "$auth_secret" "$umami_secret"
  stacks+=(destino-fisico); use destino-fisico
  compose up -d s3 --wait
  compose exec -T s3 sh -c "echo 's3.bucket.create -name notify-backup-destino-fisico' | weed shell -master=localhost:9333" >/dev/null
  dst_key=$(envval WALG_S3_ACCESS_KEY); dst_secret=$(envval WALG_S3_SECRET_KEY)
  # Cópia do bucket entre operadores (rclone entre dois endpoints S3).
  docker run --rm --network host \
    -e RCLONE_CONFIG_SRC_TYPE=s3 -e RCLONE_CONFIG_SRC_PROVIDER=Other -e RCLONE_CONFIG_SRC_ENDPOINT="http://127.0.0.1:$((S3_BASE + 0))" \
    -e RCLONE_CONFIG_SRC_ACCESS_KEY_ID="$src_key" -e RCLONE_CONFIG_SRC_SECRET_ACCESS_KEY="$src_secret" -e RCLONE_CONFIG_SRC_REGION=us-east-1 \
    -e RCLONE_CONFIG_DST_TYPE=s3 -e RCLONE_CONFIG_DST_PROVIDER=Other -e RCLONE_CONFIG_DST_ENDPOINT="http://127.0.0.1:$((S3_BASE + 2))" \
    -e RCLONE_CONFIG_DST_ACCESS_KEY_ID="$dst_key" -e RCLONE_CONFIG_DST_SECRET_ACCESS_KEY="$dst_secret" -e RCLONE_CONFIG_DST_REGION=us-east-1 \
    "$RCLONE" sync --checksum --s3-no-check-bucket "SRC:$src_bucket/pg" "DST:notify-backup-destino-fisico/pg" -v 2>&1 | tail -4
  objs_src=$(docker run --rm --network host -e RCLONE_CONFIG_S_TYPE=s3 -e RCLONE_CONFIG_S_PROVIDER=Other -e RCLONE_CONFIG_S_ENDPOINT="http://127.0.0.1:$S3_BASE" -e RCLONE_CONFIG_S_ACCESS_KEY_ID="$src_key" -e RCLONE_CONFIG_S_SECRET_ACCESS_KEY="$src_secret" "$RCLONE" size S:$src_bucket/pg --json)
  objs_dst=$(docker run --rm --network host -e RCLONE_CONFIG_S_TYPE=s3 -e RCLONE_CONFIG_S_PROVIDER=Other -e RCLONE_CONFIG_S_ENDPOINT="http://127.0.0.1:$((S3_BASE + 2))" -e RCLONE_CONFIG_S_ACCESS_KEY_ID="$dst_key" -e RCLONE_CONFIG_S_SECRET_ACCESS_KEY="$dst_secret" "$RCLONE" size S:notify-backup-destino-fisico/pg --json)
  [ "$objs_src" = "$objs_dst" ] || fail "o bucket copiado difere: $objs_src x $objs_dst"
  ok "[B] bucket copiado: $objs_dst"
  lap "bucket de backup copiado entre operadores"

  # Restauração, como em docs/deploy.md ("Restaurar o banco"), numa pasta vazia.
  mkdir -p "$work/destino-fisico-data/postgres"
  # shellcheck disable=SC2016 # as variáveis são do contêiner, não deste shell
  compose run --rm --no-deps --entrypoint sh postgres -c \
    'chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA" && su postgres -c "notify-restore LATEST"' >/dev/null
  lap "notify-restore LATEST"
  compose up -d postgres --wait >/dev/null
  [ "$(pgsql notify -c 'select pg_is_in_recovery()')" = f ] || fail "banco restaurado em recuperação"
  lap "Postgres aplicou o WAL e abriu"
  # O físico traz também os papéis, com as senhas da ORIGEM: o .env do destino não bate.
  if pg_net notify "$(envval NOTIFY_DB_PASSWORD)" notify 'select 1' >/dev/null 2>&1; then
    fail "[B] esperava que a senha do destino NÃO entrasse depois do restore físico"
  else
    ok "[B] ACHADO: depois do restore físico, a senha NOTIFY_DB_PASSWORD do destino NÃO entra (as senhas dos papéis vieram da origem)"
  fi
  # Correção do procedimento: alinhar as senhas dos papéis ao .env do destino.
  for r in postgres:POSTGRES_PASSWORD notify:NOTIFY_DB_PASSWORD umami:UMAMI_DB_PASSWORD; do
    printf "ALTER ROLE %s PASSWORD '%s'\n" "${r%%:*}" "$(envval "${r##*:}")" | compose exec -T -u postgres postgres psql -X -v ON_ERROR_STOP=1 -d postgres >/dev/null
  done
  compose exec -T postgres sh < "$repo/infra/compose/postgres/init/02-ingest-roles.sh" >/dev/null
  ok "[B] ALTER ROLE (postgres, notify, umami) e 02-ingest-roles.sh alinharam as senhas ao .env do destino"
  fp_phys="$work/fp-fisico.txt"
  COMPOSE="docker compose -p $P --env-file $ENVF -f prod.yaml -f test/prod-test.yaml -f test/migration-test.yaml" \
    "$repo/infra/scripts/db-fingerprint.sh" > "$fp_phys"
  diff -u "$fp_origin" "$fp_phys" > /dev/null || { diff -u "$fp_origin" "$fp_phys" | head -30; fail "impressão digital do destino físico difere"; }
  ok "[B] impressão digital idêntica"
  compose up -d --wait portal umami >/dev/null
  lap "aplicação do destino físico no ar"
  verify_destination
fi
echo "migração de dados: tudo certo"
