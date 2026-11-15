#!/usr/bin/env bash
# Importa os dumps de db-export.sh no Postgres de DESTINO (#54, docs/transferencia.md).
#
# O destino já passou pelo `site.yml` (Postgres com os papéis criados pelos
# scripts de init). Este script PARA a aplicação, APAGA e recria os bancos
# notify e umami (o app novo, ao subir, criou tabelas vazias e o restore bateria
# nelas), restaura os dumps, reaplica o 02-ingest-roles.sh e confere a
# impressão digital contra a da origem. Só depois disso suba a aplicação.
#
#   infra/scripts/db-import.sh --yes /root/migracao
#
# Rode na VM de destino como root, em /opt/notify/infra/compose. Destrutivo:
# exige --yes.
set -euo pipefail

if [ "${1:-}" != "--yes" ] || [ -z "${2:-}" ]; then
  echo "uso: db-import.sh --yes <pasta-com-os-dumps>  (APAGA os bancos notify e umami do destino)" >&2
  exit 2
fi
dir="$2"
COMPOSE="${COMPOSE:-docker compose -f prod.yaml}"
here="$(cd "$(dirname "$0")" && pwd)"
pg() { $COMPOSE exec -T -u postgres postgres "$@"; }

(cd "$dir" && sha256sum -c sha256sums)

$COMPOSE stop caddy portal ingest-web umami >/dev/null 2>&1 || true

# Mesmos comandos do infra/compose/postgres/init/01-databases.sh, sem criar os
# papéis (já existem). WITH (FORCE) derruba conexões que sobraram.
pg psql -X -v ON_ERROR_STOP=1 -d postgres <<'SQL'
DROP DATABASE IF EXISTS umami WITH (FORCE);
DROP DATABASE IF EXISTS notify WITH (FORCE);
CREATE DATABASE umami OWNER umami;
CREATE DATABASE notify OWNER notify;
REVOKE ALL ON DATABASE umami FROM PUBLIC;
REVOKE ALL ON DATABASE notify FROM PUBLIC;
SQL

for db in notify umami; do
  echo "importando $db..."
  pg pg_restore --no-password --exit-on-error -d "$db" < "$dir/$db.dump"
done

# CONNECT dos usuários do gateway e do coletor é permissão do banco, que o dump
# não leva. O script é idempotente.
$COMPOSE exec -T postgres sh < "$here/../compose/postgres/init/02-ingest-roles.sh"

echo "conferindo contra a origem..."
"$here/db-fingerprint.sh" > "$dir/fingerprint-destino.txt"
# Papéis e ACL dos bancos podem diferir só se alguém mexeu; o resto tem de ser idêntico.
if diff -u "$dir/fingerprint.txt" "$dir/fingerprint-destino.txt"; then
  echo "OK: destino idêntico à origem. Pode subir a aplicação."
else
  echo "FALHOU: o destino difere da origem (diff acima). NÃO suba a aplicação." >&2
  exit 1
fi
