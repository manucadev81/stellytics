# loadtest

Gerador de carga do pipeline web e do pipeline de API (issue #51). Resultado e
método em `docs/relatorios/carga-1m-dia.md`.

```sh
cd services
go build -o /tmp/loadtest ./loadtest
/tmp/loadtest setup    -notify-db ... -umami-url ... -out state.json   # site no Umami, tenant, serviços e chaves
/tmp/loadtest web      -state state.json -profile "11.6x10m,116x5m"    # GET /c, prova de JS, POST /e
/tmp/loadtest api      -state state.json -profile "11.6x10m,116x5m"    # OTLP/HTTP (métricas e spans)
/tmp/loadtest teardown -state state.json
```

- **Perfil:** lista de degraus `taxaxduração` (page views ou requisições por
  segundo). Chegada de Poisson, sem esperar as respostas (um servidor lento
  aparece como latência e erro). Chegadas além de `-max-inflight` contam como
  saturação do gerador.
- **web:** cada visitante tem IP, UA, idioma e tela fixos, num /24 próprio; o IP
  vai em `X-Forwarded-For`, então o gateway precisa de `TRUSTED_PROXIES` com o
  endereço do gerador. A prova de JS é resolvida como o script faz (o pacote
  `internal/proof` não pode ser importado de fora de `services/ingest-web`, por
  isso o algoritmo é repetido em `web.go`). No fim, conta `website_event` no
  banco do Umami (`-umami-db`) contra os 204 recebidos.
- **api:** simula serviços de API que exportam histogramas delta
  (`-modes metrics`) ou um span de servidor por requisição (`-modes spans`),
  com `Authorization: Bearer` de chaves criadas no `setup`. No fim compara
  `request_count` em `api_metric_1m` com o que foi enviado com sucesso.
- `-watch nome=pid,...` e `-docker contêiner,...` amostram CPU e memória a cada
  2 s e resumem por degrau (Linux; o docker usa `docker stats`).

## Segurança

- `setup`, `teardown`, `web` e `api` recusam qualquer alvo que não seja
  loopback (`-notify-db`, `-umami-url`, `-gateway`, `-umami-db`, `-collector`)
  sem `-allow-remote`, e imprimem host e banco antes de agir. Não aponte para
  produção.
- O arquivo de estado (`state.json`, no `.gitignore`) guarda as chaves de
  ingestão `ntf_...` completas, em modo 0600. Apague-o depois do `teardown`.
- O `setup` grava o estado a cada passo; se falhar no meio, o `teardown` apaga
  o que foi criado. O `teardown` só apaga usuário `loadtest-*` e os tenants
  `<usuário>-web` e `<usuário>-api`, e aborta se o estado não bater com o banco.
- Os IPs dos visitantes simulados ficam em 10.0.0.0/8.
