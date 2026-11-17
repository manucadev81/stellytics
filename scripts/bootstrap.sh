#!/usr/bin/env bash
set -euo pipefail

# Script de bootstrap para o ambiente local de desenvolvimento do stellytics.
# Configura o .env, dependências, credenciais da conta de serviço do Umami
# e inicializa a stack completa via Docker Compose.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FORCE_REGEN=false
for arg in "$@"; do
  case "$arg" in
    --force)
      FORCE_REGEN=true
      ;;
    -h|--help)
      echo "Uso: ./scripts/bootstrap.sh [--force]"
      echo ""
      echo "Opções:"
      echo "  --force    Gera uma chave nova do Umami mesmo se já existir no .env (não revoga a anterior)"
      exit 0
      ;;
    *)
      echo "Opção desconhecida: $arg" >&2
      exit 1
      ;;
  esac
done

echo "=== [1/5] Verificando pré-requisitos ==="
for cmd in docker pnpm node curl; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Erro: '$cmd' não está instalado ou não foi encontrado no PATH." >&2
    exit 1
  fi
done

if ! docker compose version >/dev/null 2>&1; then
  echo "Erro: Docker Compose (v2) não foi encontrado." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Erro: O daemon do Docker não está em execução." >&2
  exit 1
fi

if [ ! -d "node_modules" ]; then
  echo "Dependências (node_modules) não encontradas. Executando 'pnpm install'..."
  pnpm install
fi

echo "=== [2/5] Configurando variáveis de ambiente (.env) ==="
if [ ! -f .env ]; then
  echo "Criando .env a partir de .env.example..."
  cp .env.example .env
else
  echo ".env já existe."
fi

# Parse the simple dotenv syntax used by Compose, including quoted values and
# inline comments. Do not source .env as shell code.
ENV_VALUES="$(node --input-type=module -e '
  import fs from "node:fs";
  const values = {};
  for (const line of fs.readFileSync(".env", "utf8").split(/\r?\n/)) {
    const m = line.match(/^\s*(?:export\s+)?(UMAMI_PORT|UMAMI_API_KEY)\s*=\s*(.*)\s*$/);
    if (!m) continue;
    let value = m[2];
    if (value.startsWith("\"") || value.startsWith("\047")) {
      const q = value[0];
      const end = value.indexOf(q, 1);
      value = end < 0 ? value.slice(1) : value.slice(1, end);
    } else value = value.replace(/\s+#.*$/, "").trim();
    values[m[1]] = value;
  }
  console.log(JSON.stringify(values));
')"
UMAMI_PORT="$(node -e 'const e=JSON.parse(process.argv[1]); console.log(e.UMAMI_PORT || "3001")' "$ENV_VALUES")"
CURRENT_API_KEY="$(node -e 'const e=JSON.parse(process.argv[1]); console.log(e.UMAMI_API_KEY || "")' "$ENV_VALUES")"
case "$UMAMI_PORT" in ''|*[!0-9]*) echo "Erro: UMAMI_PORT deve ser uma porta numérica." >&2; exit 1 ;; esac
[ "$UMAMI_PORT" -ge 1 ] && [ "$UMAMI_PORT" -le 65535 ] || { echo "Erro: UMAMI_PORT fora do intervalo válido." >&2; exit 1; }

NEED_KEY_GEN=false
if [ -z "$CURRENT_API_KEY" ] || [ "$FORCE_REGEN" = true ]; then
  NEED_KEY_GEN=true
fi

if [ "$NEED_KEY_GEN" = true ]; then
  echo "=== [3/5] Subindo dependências base para configuração da API Key ==="
  docker compose -f compose.yaml -f infra/compose/umami-port.yaml up -d --wait postgres mailpit umami

  echo "Aguardando Umami responder em http://127.0.0.1:${UMAMI_PORT}/api/heartbeat..."
  max_retries=30
  retry_count=0
  until curl -fsS "http://127.0.0.1:${UMAMI_PORT}/api/heartbeat" >/dev/null 2>&1; do
    retry_count=$((retry_count + 1))
    if [ "$retry_count" -ge "$max_retries" ]; then
      echo "Erro: Tempo limite aguardando o Umami." >&2
      exit 1
    fi
    sleep 1
  done

  echo "=== [4/5] Configurando conta de serviço do Umami ==="
  echo "Gerando chave de serviço para o portal..."
  if [ -z "${UMAMI_ADMIN_PASSWORD:-}" ]; then
    if [ -t 0 ]; then
      read -r -s -p "Senha de administrador do Umami: " UMAMI_ADMIN_PASSWORD
      echo
    else
      echo "Defina UMAMI_ADMIN_PASSWORD para autenticar no Umami." >&2
      exit 1
    fi
  fi
  NEW_KEY="$(UMAMI_ADMIN_PASSWORD="$UMAMI_ADMIN_PASSWORD" UMAMI_URL="http://127.0.0.1:${UMAMI_PORT}" node apps/portal/scripts/umami-service-account.mjs | tr -d '\r\n ')"

  if [ -z "$NEW_KEY" ]; then
    echo "Erro: Falha ao obter a chave de serviço do Umami." >&2
    exit 1
  fi

  # Gravar com segurança no .env usando Node (evita problemas de delimitador e escape do sed)
  node -e '
    const fs = require("node:fs");
    const key = process.argv[1];
    let env = fs.readFileSync(".env", "utf8");
    const regex = /^UMAMI_API_KEY=.*$/m;
    if (regex.test(env)) {
      env = env.replace(regex, () => "UMAMI_API_KEY=" + key);
    } else {
      env += (env.endsWith("\n") || env.length === 0 ? "" : "\n") + "UMAMI_API_KEY=" + key + "\n";
    }
    const tmp = `.env.tmp-${process.pid}`;
    fs.writeFileSync(tmp, env, { encoding: "utf8", mode: 0o600 });
    fs.renameSync(tmp, ".env");
  ' "$NEW_KEY"

  echo "Chave de serviço salva no .env."
else
  echo "=== [3/5] Dependências base já configuradas ==="
  echo "=== [4/5] Chave UMAMI_API_KEY já presente no .env (use --force para regenerar) ==="
fi

echo "=== [5/5] Subindo a stack de desenvolvimento completa ==="
docker compose up -d --wait

echo ""
echo "=========================================================="
echo "🚀 Ambiente stellytics pronto e em execução!"
echo "=========================================================="
echo "Portal Web:            http://localhost:3000"
echo "  - Contas fixas: usadas apenas pelo seed local; troque as senhas antes de expor o portal."
echo "Gateway Web:           http://localhost:3002"
echo "Coletor API (OTLP):    http://localhost:4318 (HTTP) | localhost:4317 (gRPC)"
echo "Mailpit (E-mails):     http://localhost:8025"
echo "PostgreSQL:            localhost:5432 (stellytics/stellytics e umami/umami)"
echo "=========================================================="
echo "Dica: Para parar os serviços, execute: docker compose down"
echo "Para reiniciar os dados do zero:      docker compose down -v"
