#!/usr/bin/env bash
set -Eeuo pipefail

readonly VENV_DIR=/opt/stellytics-ansible
readonly BIN_DIR=/usr/local/bin
readonly TOOLS=(ansible ansible-playbook ansible-inventory)

fail() {
  printf 'init: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Uso: sudo ./init.sh

Prepara esta máquina Debian/Ubuntu para executar ./deploy.sh:
- instala Python, OpenSSH client e certificados;
- instala ansible-core em um ambiente isolado em /opt/stellytics-ansible;
- disponibiliza os comandos Ansible em /usr/local/bin.

O script não instala Docker nem altera a VPS de produção. O playbook Ansible
instala e configura o Docker no host de produção durante ./deploy.sh.
USAGE
  exit 0
fi

[[ $# -eq 0 ]] || fail "argumento não reconhecido; use --help"
[[ "$EUID" -eq 0 ]] || fail "execute com sudo: sudo ./init.sh"
command -v apt-get >/dev/null 2>&1 || fail "este script requer Debian ou Ubuntu com apt-get"

printf 'Instalando pré-requisitos do Ansible...\n'
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates openssh-client python3 python3-venv

python3 -c '
import sys
if sys.version_info < (3, 12):
    raise SystemExit(f"init: ansible-core 2.21+ requer Python 3.12 ou mais novo; encontrado {sys.version.split()[0]}")
'

printf 'Instalando ansible-core em ambiente isolado...\n'
python3 -m venv "$VENV_DIR"
"$VENV_DIR/bin/python" -m pip install --upgrade 'ansible-core>=2.21,<3'

install -d -m 0755 "$BIN_DIR"
for tool in "${TOOLS[@]}"; do
  target="$VENV_DIR/bin/$tool"
  link="$BIN_DIR/$tool"
  [[ -x "$target" ]] || fail "instalação incompleta: $target não foi criado"
  if [[ -e "$link" && ! -L "$link" ]]; then
    fail "$link já existe como arquivo; remova ou renomeie-o antes de repetir"
  fi
  ln -sfn "$target" "$link"
done

printf '\nAnsible instalado:\n'
ansible-playbook --version
printf '\nAgora execute ./deploy.sh como seu usuário normal, sem sudo.\n'
