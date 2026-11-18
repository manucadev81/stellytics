#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$ROOT_DIR/infra/compose/prod-vps.yaml"
ENV_FILE="$ROOT_DIR/.env.production"

fail() {
  printf 'deploy: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Uso: sudo ./deploy.sh

Valida .env.production, constrói as imagens da aplicação e sobe a stack mínima
de produção nesta VPS: portal, gateway web, API OTLP, Umami, Postgres, rollup e
Caddy. O Postgres e os dados TLS ficam em volumes Docker locais.
USAGE
  exit 0
fi

[[ $# -eq 0 ]] || fail "argumento não reconhecido; use --help"
[[ "$EUID" -eq 0 ]] || fail "execute com sudo: sudo ./deploy.sh"
command -v docker >/dev/null 2>&1 || fail "Docker não encontrado; execute sudo ./init.sh primeiro"
command -v python3 >/dev/null 2>&1 || fail "Python 3 não encontrado; execute sudo ./init.sh primeiro"
docker compose version >/dev/null 2>&1 || fail "Docker Compose não encontrado; execute sudo ./init.sh primeiro"
docker info >/dev/null 2>&1 || fail "o daemon Docker não está em execução"
[[ -f "$ENV_FILE" ]] || fail "arquivo ausente: $ENV_FILE (execute sudo ./init.sh primeiro)"
[[ -f "$COMPOSE_FILE" ]] || fail "Compose de VPS ausente: $COMPOSE_FILE"

python3 - "$ENV_FILE" <<'PY'
import os
import stat
import sys

path = sys.argv[1]
mode = stat.S_IMODE(os.stat(path).st_mode)
if mode & 0o077:
    raise SystemExit(f"deploy: {path} permite leitura por grupo/outros (modo {mode:04o}); rode sudo chmod 600 {path}")

values = {}
with open(path, encoding="utf-8") as stream:
    for line in stream:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in line:
            continue
        key, value = line.rstrip("\n").split("=", 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key.strip()] = value

required = (
    "POSTGRES_PASSWORD", "UMAMI_DB_PASSWORD", "STELLYTICS_DB_PASSWORD",
    "STELLYTICS_INGEST_DB_PASSWORD", "STELLYTICS_COLLECTOR_DB_PASSWORD",
    "UMAMI_APP_SECRET", "PORTAL_AUTH_SECRET", "INGEST_PROOF_SECRET",
    "PORTAL_HOST", "PORTAL_ALIAS_HOST", "INGEST_HOST", "API_HOST",
    "ACME_EMAIL", "SMTP_URL", "MAIL_FROM",
)
missing = [key for key in required if not values.get(key)]
if missing:
    raise SystemExit("deploy: preencha estes campos em .env.production: " + ", ".join(missing))

if values["POSTGRES_PASSWORD"] in {"postgres", "password", "CHANGE_ME"}:
    raise SystemExit("deploy: POSTGRES_PASSWORD ainda usa um valor inseguro")
PY

compose=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
printf 'Validando a configuração do Compose...\n'
"${compose[@]}" config --quiet

printf 'Construindo e iniciando a stack mínima da VPS...\n'
"${compose[@]}" up -d --build --wait --remove-orphans

printf '\nEstado dos serviços:\n'
"${compose[@]}" ps
