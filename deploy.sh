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

python3 "$ROOT_DIR/infra/scripts/production-env.py" validate "$ENV_FILE"

compose=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
printf 'Validando a configuração do Compose...\n'
"${compose[@]}" config --quiet

printf 'Construindo e iniciando a stack mínima da VPS...\n'
"${compose[@]}" up -d --build --wait --remove-orphans

printf '\nEstado dos serviços:\n'
"${compose[@]}" ps
