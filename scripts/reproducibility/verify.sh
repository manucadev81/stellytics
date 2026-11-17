#!/usr/bin/env bash
# Reconstrói uma imagem Go de uma revisão e compara as camadas com uma imagem
# imutável da release. Requer Docker Buildx, Python 3, git e acesso ao registry.
set -euo pipefail
if [ "$#" != 3 ]; then
  echo "uso: $0 <revisão> <serviço> <imagem@sha256:digest>" >&2
  exit 2
fi
revision=$1 service=$2 reference=$3
case "$service" in
  ingest-web|collector|rollup|replication-monitor) ;;
  *) echo "serviço Go não suportado: $service" >&2; exit 2 ;;
esac
if [[ "$reference" =~ @sha256:[a-f0-9]{64}$ ]]; then
  remote=1
elif [[ "$reference" =~ ^sha256:[a-f0-9]{64}$ ]]; then
  remote=0  # ID local imutável, para ensaiar sem uma release publicada.
else
  echo 'use imagem@sha256:digest da release ou um ID local sha256' >&2
  exit 2
fi
root=$(git rev-parse --show-toplevel)
commit=$(git rev-parse --verify "${revision}^{commit}")
work=$(mktemp -d "${TMPDIR:-/tmp}/stellytics-repro.XXXXXXXX")
suffix=${work##*.}
builder="stellytics-repro-${suffix,,}"
tag="stellytics-repro:${suffix,,}"
cleanup() {
  status=$?
  docker buildx rm "$builder" >/dev/null 2>&1 || true
  docker image rm "$tag" >/dev/null 2>&1 || true
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT
mkdir "$work/source"
git -C "$root" archive "$commit" | tar -x -C "$work/source"
printf 'revisão: %s; serviço: %s; imagem: %s; plataforma: linux/amd64\n' "$commit" "$service" "$reference"
if [ "$remote" = 1 ]; then
  docker pull --platform linux/amd64 "$reference"
fi
docker image save -o "$work/original.tar" "$reference"
docker buildx create --name "$builder" --driver docker-container >/dev/null
docker buildx build --builder "$builder" --platform linux/amd64 --no-cache --provenance=false --load \
  -t "$tag" -f "$work/source/services/$service/Dockerfile" "$work/source"
docker image save -o "$work/rebuilt.tar" "$tag"
python3 "$root/scripts/reproducibility/compare_layers.py" "$work/original.tar" "$work/rebuilt.tar"
