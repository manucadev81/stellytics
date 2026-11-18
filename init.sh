#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$ROOT_DIR/.env.production"
readonly DOCKER_KEY_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88

fail() {
  printf 'init: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Uso: sudo ./init.sh

Prepara uma VPS Debian/Ubuntu para rodar a instalação mínima:
- instala Docker Engine e Docker Compose pelo repositório oficial;
- aplica a configuração de rede e rotação de logs usada pela stack;
- cria .env.production com segredos aleatórios, sem sobrescrever arquivo existente.

Depois, configure domínios e SMTP em .env.production e rode sudo ./deploy.sh.
Os dados do Postgres e certificados ficam em volumes Docker locais; não há
backup externo nesta instalação.
USAGE
  exit 0
fi

[[ $# -eq 0 ]] || fail "argumento não reconhecido; use --help"
[[ "$EUID" -eq 0 ]] || fail "execute com sudo: sudo ./init.sh"
command -v apt-get >/dev/null 2>&1 || fail "este script requer Debian ou Ubuntu com apt-get"

# Identifica a distribuição e arquitetura antes de configurar o repositório.
. /etc/os-release
case "${ID:-}" in
  debian|ubuntu) docker_repo_os="$ID" ;;
  *) fail "distribuição não suportada: ${ID:-desconhecida}; use Debian ou Ubuntu" ;;
esac
case "$(dpkg --print-architecture)" in
  amd64|arm64) docker_arch="$(dpkg --print-architecture)" ;;
  *) fail "arquitetura não suportada pelo repositório Docker" ;;
esac
[[ -n "${VERSION_CODENAME:-}" ]] || fail "não foi possível identificar o codinome da distribuição"

printf 'Instalando pré-requisitos do Docker...\n'
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl gnupg
install -d -m 0755 /etc/apt/keyrings
curl -fsSL "https://download.docker.com/linux/$docker_repo_os/gpg" -o /etc/apt/keyrings/docker.asc
docker_key_fingerprint="$(gpg --show-keys --with-colons /etc/apt/keyrings/docker.asc | awk -F: '/^fpr:/ {print $10; exit}')"
[[ "$docker_key_fingerprint" == "$DOCKER_KEY_FINGERPRINT" ]] || fail "a impressão digital da chave do repositório Docker não confere"
chmod 0644 /etc/apt/keyrings/docker.asc
printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
  "$docker_arch" "$docker_repo_os" "$VERSION_CODENAME" > /etc/apt/sources.list.d/docker.list

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin python3

printf 'Configurando o Docker para esta stack...\n'
daemon_config_result="$(python3 - <<'PY'
import json
import os
import tempfile

path = "/etc/docker/daemon.json"
os.makedirs(os.path.dirname(path), mode=0o755, exist_ok=True)
config = {}
if os.path.exists(path):
    with open(path, encoding="utf-8") as stream:
        config = json.load(stream)

desired = {
    "userland-proxy": False,
    "ipv6": True,
    "ip6tables": True,
    "live-restore": True,
    "log-driver": "json-file",
    "log-opts": {"max-size": "20m", "max-file": "5"},
}
changed = any(config.get(key) != value for key, value in desired.items())
config.update(desired)
if changed:
    fd, temporary = tempfile.mkstemp(prefix="daemon.json.", dir=os.path.dirname(path))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(config, stream, indent=2)
            stream.write("\n")
        os.chmod(temporary, 0o644)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
print("changed" if changed else "unchanged")
PY
)" || fail "não foi possível atualizar a configuração do Docker"

if [[ "$daemon_config_result" == changed ]] && systemctl is-active --quiet docker; then
  systemctl restart docker
else
  systemctl enable --now docker
fi

docker compose version >/dev/null || fail "Docker Compose não ficou disponível"

if [[ ! -e "$ENV_FILE" ]]; then
  [[ -f "$ROOT_DIR/.env.production.example" ]] || fail "arquivo modelo ausente: $ROOT_DIR/.env.production.example"
  umask 077
  install -m 0600 "$ROOT_DIR/.env.production.example" "$ENV_FILE"
  python3 - "$ENV_FILE" <<'PY'
import os
import secrets
import sys
import tempfile

path = sys.argv[1]
secret_names = {
    "POSTGRES_PASSWORD",
    "UMAMI_DB_PASSWORD",
    "STELLYTICS_DB_PASSWORD",
    "STELLYTICS_INGEST_DB_PASSWORD",
    "STELLYTICS_COLLECTOR_DB_PASSWORD",
    "UMAMI_APP_SECRET",
    "PORTAL_AUTH_SECRET",
    "INGEST_PROOF_SECRET",
}
lines = []
for line in open(path, encoding="utf-8"):
    if "=" in line and not line.lstrip().startswith("#"):
        key, value = line.rstrip("\n").split("=", 1)
        if key in secret_names and not value:
            line = f"{key}={secrets.token_hex(32)}\n"
    lines.append(line)

fd, temporary = tempfile.mkstemp(prefix=".env.production.", dir=os.path.dirname(path))
try:
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.writelines(lines)
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
PY
  printf 'Criei .env.production com segredos aleatórios e permissão 0600.\n'
else
  chmod 0600 "$ENV_FILE"
  printf '.env.production existente preservado; permissões ajustadas para 0600.\n'
fi

printf '\nDocker e Compose estão prontos. Configure domínios e SMTP em:\n%s\n' "$ENV_FILE"
printf 'Depois, execute: sudo %s/deploy.sh\n' "$ROOT_DIR"
