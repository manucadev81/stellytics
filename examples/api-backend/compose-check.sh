#!/bin/sh
# Confere o backend de exemplo no Compose (#35): cria um tenant com uma chave
# de ingestão, sobe infra/compose/api-example.yaml, faz requisições e espera as
# linhas em api_metric_1m. Rodar na raiz do repositório, com o compose.yaml no
# ar. Apaga o tenant no fim. O e2e da #36 vai além disso.
set -eu

psql() { docker compose exec -T postgres psql -U postgres -d notify -v ON_ERROR_STOP=1 -Atq "$@"; }

suffix=$(tr -dc 'a-z0-9' </dev/urandom | head -c 10)
prefix=$(tr -dc 'a-z2-7' </dev/urandom | head -c 8)
secret=$(tr -dc 'A-Za-z0-9_-' </dev/urandom | head -c 43)
key="ntf_${prefix}_${secret}"

tenant=$(psql -c "
  INSERT INTO \"user\" (id, name, email, email_verified)
    VALUES ('api-example-$suffix', 'api-example', 'api-example-$suffix@example.invalid', true);
  INSERT INTO tenant (name, slug, status) VALUES ('api-example-$suffix', 'api-example-$suffix', 'active') RETURNING id;")
service=$(psql -c "INSERT INTO api_service (tenant_id, name) VALUES ('$tenant', 'example') RETURNING id")
psql -c "INSERT INTO ingest_key (api_service_id, tenant_id, prefix, key_hash, created_by)
  VALUES ('$service', '$tenant', '$prefix', sha256(convert_to('$key', 'UTF8')), 'api-example-$suffix')"

cleanup() {
  NOTIFY_INGEST_KEY=x docker compose -f compose.yaml -f infra/compose/api-example.yaml \
    rm -sf example-api tenant-otelcol >/dev/null 2>&1 || true
  psql -c "DELETE FROM api_metric_1m WHERE tenant_id = '$tenant';
    DELETE FROM ingest_key WHERE tenant_id = '$tenant';
    DELETE FROM api_service WHERE tenant_id = '$tenant';
    DELETE FROM tenant WHERE id = '$tenant';
    DELETE FROM \"user\" WHERE id = 'api-example-$suffix';" || true
}
trap cleanup EXIT

NOTIFY_INGEST_KEY="$key" docker compose -f compose.yaml -f infra/compose/api-example.yaml \
  up -d --build --no-deps --wait --wait-timeout 180 example-api tenant-otelcol

base=http://127.0.0.1:${EXAMPLE_API_PORT:-4010}
for _ in 1 2 3; do
  curl -fsS "$base/accounts/GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7" >/dev/null
  curl -fsS "$base/ledgers/123" >/dev/null
  curl -s -o /dev/null "$base/x/42"
done

query="SELECT coalesce(sum(request_count), 0) FROM api_metric_1m WHERE tenant_id = '$tenant'"
for _ in $(seq 1 30); do
  total=$(psql -c "$query")
  [ "$total" -ge 9 ] && break
  sleep 2
done
psql -c "SELECT route, method, status_code, request_count FROM api_metric_1m WHERE tenant_id = '$tenant' ORDER BY route"
if [ "$total" -lt 9 ]; then
  echo "esperava 9 requisições em api_metric_1m, achei $total"
  docker compose -f compose.yaml -f infra/compose/api-example.yaml logs --tail 50 tenant-otelcol example-api || true
  exit 1
fi
# A rota sem padrão conhecido foi normalizada pelo coletor do notify.
psql -c "SELECT count(*) FROM api_metric_1m WHERE tenant_id = '$tenant' AND route = '/x/{id}' AND status_code = 404" | grep -qx 1
echo "ok: $total requisições"
