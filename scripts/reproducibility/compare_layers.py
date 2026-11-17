#!/usr/bin/env python3
"""Compare o conteúdo das camadas de dois arquivos `docker image save`.

Ignora timestamps, mas compara ordem das camadas, caminhos, tipos, permissões,
proprietários, links e SHA-256 de cada arquivo. Não compara a configuração OCI.
"""
import hashlib
import json
import sys
import tarfile


def layers(path):
    with tarfile.open(path, mode="r:*") as image:
        manifest = json.load(image.extractfile("manifest.json"))
        if len(manifest) != 1:
            raise ValueError("esperada exatamente uma imagem em cada arquivo")
        result = []
        for layer_path in manifest[0]["Layers"]:
            entries = []
            seen = set()
            with image.extractfile(layer_path) as raw:
                with tarfile.open(fileobj=raw, mode="r:*") as layer:
                    for item in layer:
                        if item.name in seen:
                            raise ValueError(f"caminho duplicado em {layer_path}: {item.name}")
                        seen.add(item.name)
                        content = None
                        if item.isfile():
                            digest = hashlib.sha256()
                            with layer.extractfile(item) as f:
                                for chunk in iter(lambda: f.read(1024 * 1024), b""):
                                    digest.update(chunk)
                            content = digest.hexdigest()
                        entries.append((item.name, item.type.decode("latin1"), item.mode,
                                        item.uid, item.gid, item.linkname, item.size,
                                        item.devmajor, item.devminor,
                                        tuple(sorted((k, v) for k, v in item.pax_headers.items()
                                                     if k not in {"mtime", "atime", "ctime"})), content))
            result.append(sorted(entries))
        return result


def main():
    if len(sys.argv) != 3:
        raise SystemExit("uso: compare_layers.py original.tar reconstruida.tar")
    left, right = (layers(path) for path in sys.argv[1:])
    if len(left) != len(right):
        raise SystemExit(f"número de camadas difere: {len(left)} != {len(right)}")
    for index, (a, b) in enumerate(zip(left, right), 1):
        if a != b:
            a_only, b_only = set(a) - set(b), set(b) - set(a)
            print(f"camada {index} difere", file=sys.stderr)
            for label, entries in (("original", a_only), ("reconstruída", b_only)):
                for entry in sorted(entries)[:10]:
                    print(f"  {label}: {entry[0]}", file=sys.stderr)
            raise SystemExit(1)
    print(f"conteúdo igual em {len(left)} camadas (timestamps e configuração OCI não comparados)")


if __name__ == "__main__":
    main()
