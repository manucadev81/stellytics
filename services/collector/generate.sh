#!/bin/sh
# Gera o código da distribuição (collector/cmd/notify-collector) com o OCB.
#
# O OCB sempre cria um go.mod próprio na pasta de saída. Como o ADR 0002 pede um
# módulo Go só em services/, o script gera numa pasta temporária e copia só os
# arquivos .go, que compilam dentro do módulo services. Rode de dentro de
# services/ depois de mudar collector/builder-config.yaml ou a versão do OCB;
# o CI confere que o resultado está commitado.
set -eu

OCB_VERSION=v0.164.0
out=collector/cmd/notify-collector
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

sed "s|output_path: .*|output_path: $tmp|" collector/builder-config.yaml > "$tmp/builder-config.yaml"
go run "go.opentelemetry.io/collector/cmd/builder@$OCB_VERSION" \
  --config "$tmp/builder-config.yaml" --skip-compilation --skip-get-modules >/dev/null

mkdir -p "$out"
rm -f "$out"/*.go
for f in "$tmp"/*.go; do
  { echo "// Código gerado pelo OCB ($OCB_VERSION) a partir de collector/builder-config.yaml. Não edite: rode collector/generate.sh."; echo; cat "$f"; } > "$out/$(basename "$f")"
done
gofmt -w "$out"
