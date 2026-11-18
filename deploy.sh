#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ANSIBLE_DIR="$ROOT_DIR/infra/ansible"
INVENTORY="$ANSIBLE_DIR/inventory/production.yml"
SECRETS="$ANSIBLE_DIR/secrets.yml"

fail() {
  printf 'deploy: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Uso: ./deploy.sh

Aplica o playbook de produção da aplicação via Ansible. Antes, provisiona a
infraestrutura, publica as imagens assinadas e configura:
  infra/ansible/inventory/production.yml
  infra/ansible/secrets.yml

O script valida os arquivos e a sintaxe do playbook antes de iniciar o deploy.
Para provisionar a Hetzner, siga docs/deploy.md (OpenTofu é uma etapa separada).
USAGE
  exit 0
fi

[[ $# -eq 0 ]] || fail "argumentos não reconhecidos; use --help"
command -v ansible-playbook >/dev/null 2>&1 || fail "ansible-playbook não encontrado (instale ansible-core conforme docs/deploy.md)"
command -v ansible-inventory >/dev/null 2>&1 || fail "ansible-inventory não encontrado (instale ansible-core conforme docs/deploy.md)"
command -v python3 >/dev/null 2>&1 || fail "python3 não encontrado"
[[ -f "$INVENTORY" ]] || fail "inventário ausente: $INVENTORY (copie inventory/production.example.yml e preencha)"
[[ -f "$SECRETS" ]] || fail "segredos ausentes: $SECRETS (copie secrets.example.yml e preencha)"

# O arquivo é ignorado pelo Git e contém segredos usados para gerar o .env
# protegido na VM. Recuse permissões acessíveis a grupo ou outros usuários.
python3 -c '
import os, stat, sys
mode = stat.S_IMODE(os.stat(sys.argv[1]).st_mode)
if mode & 0o077:
    raise SystemExit(f"deploy: secrets.yml está acessível a grupo/outros (modo {mode:04o}); execute chmod 600 {sys.argv[1]!r}")
' "$SECRETS"

cd "$ANSIBLE_DIR"
inventory_host="$(ansible-inventory --inventory "$INVENTORY" --host stellytics)" || fail "não foi possível carregar o host stellytics do inventário"
python3 -c '
import json, sys
host = json.loads(sys.argv[1])
address = str(host.get("ansible_host", ""))
volume = str(host.get("data_volume_device", ""))
if not address or address.startswith("203.0.113."):
    raise SystemExit("deploy: ansible_host de stellytics não está configurado com o IP real")
if not volume or any(marker in volume for marker in ("12345678", "CONFIRA")):
    raise SystemExit("deploy: data_volume_device de stellytics não está configurado com o dispositivo real")
' "$inventory_host"

printf 'Validando sintaxe do playbook e dos arquivos de produção...\n'
ansible-playbook --inventory "$INVENTORY" --syntax-check site.yml --extra-vars "@$SECRETS"

printf 'Iniciando deploy da aplicação no host stellytics...\n'
exec ansible-playbook --inventory "$INVENTORY" site.yml --extra-vars "@$SECRETS"
