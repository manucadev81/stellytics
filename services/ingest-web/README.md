# services/ingest-web

Gateway de ingestão web em Go (`net/http`). Fica entre o script de rastreamento (`packages/tracker`) e o Umami, que só deve ser acessível pela rede interna. Criado na [#61](https://github.com/romulospl/notify/issues/61).

Já faz:

- recebe o page view, confere o formato e o `websiteId` e repassa ao `/api/send` do Umami;
- repassa o `User-Agent` original, para a detecção de bots do Umami continuar valendo;
- trunca o IP (último octeto do IPv4, /48 no IPv6) antes de qualquer uso e entrega ao Umami só o IP truncado, pelo header de `CLIENT_IP_HEADER`;
- limita o tamanho do corpo (16 KiB) e a taxa por IP truncado;
- serve o script em `/s/vX.Y.Z/`.

Ainda vem: prova de execução de JS (#25), limite de fingerprints por IP (#26), testes de persistência do IP (#24) e a conferência dos nomes contra ad blockers (#23).

## Rotas

| Rota | O que faz |
|---|---|
| `POST /e` | Recebe o evento. Aceita JSON com `Content-Type` `text/plain` (o que o script usa, para evitar o preflight de CORS) ou `application/json`. Responde `204` quando o evento foi repassado. |
| `OPTIONS /e` | Preflight de CORS, para clientes que mandem `application/json`. |
| `GET /s/<caminho>` | Arquivos de `SCRIPT_DIR`, com `Cache-Control: public, max-age=31536000, immutable` e `Access-Control-Allow-Origin: *` (o SRI exige `crossorigin`). Pastas e caminhos fora de `SCRIPT_DIR` dão `404`. |
| `GET /healthz` | `200 ok`. Não consulta o banco. |

Nenhuma resposta grava cookie.

### Evento aceito

```json
{"type":"event","payload":{"website":"<uuid>","hostname":"...","url":"/caminho","referrer":"...","title":"...","language":"pt-BR","screen":"1920x1080"}}
```

- `type` precisa ser `event`, `website` um UUID e `url` não pode faltar.
- Tamanhos máximos: `url` e `referrer` 2048 bytes, `title` 500, `hostname` 253, `language` 35, `screen` 16.
- O payload repassado ao Umami é montado de novo só com esses campos. Qualquer outro (`id`, `ip`, `userAgent`, `timestamp`, `data`...) é descartado, porque o Umami deixaria o cliente trocar o IP, o UA ou a identidade da sessão com eles.
- Só os headers `Content-Type`, `User-Agent` e o de `CLIENT_IP_HEADER` chegam ao Umami.

### Respostas de erro

| Status | Quando |
|---|---|
| `400` | JSON inválido ou fora do formato acima |
| `413` | Corpo maior que 16 KiB |
| `415` | `Content-Type` diferente de `text/plain` e `application/json` |
| `429` | Taxa acima do limite para o IP truncado |
| `502` | O Umami recusou o evento ou não respondeu |
| `503` | Falha ao consultar a `ingest_site` |

## Contrato com o banco: `ingest_site`

O gateway conecta no banco `notify` com o usuário `notify_ingest` e só lê a visão `ingest_site(umami_website_id uuid, accepts_ingest boolean)` (spec do E2, seções 4 e 6). `accepts_ingest` é verdadeiro para site não arquivado de tenant ativo. As respostas, positivas e negativas, ficam em memória por `SITE_CACHE_TTL` (no máximo 1 minuto).

- Site desconhecido ou que não aceita ingestão recebe `204`, como um evento aceito, e o evento é descartado. Assim, quem testa IDs não descobre quais existem.
- O usuário `notify_ingest` vem de `infra/compose/postgres/init/02-ingest-roles.sh`, e a visão e o `GRANT` vêm da migração `apps/portal/drizzle/0002_ingest_views.sql`.
- O teste de integração (`internal/site/pg_test.go`) aplica as migrações do portal quando a visão não existe, e cria tenants e sites nas tabelas reais.

## IP do cliente

Vem do `RemoteAddr`. O `X-Forwarded-For` só é lido quando a conexão chega de um CIDR de `TRUSTED_PROXIES` (o Caddy, em produção); nesse caso o cliente é o primeiro endereço, da direita para a esquerda, que não é de um proxy confiável. O IP completo só existe na memória do request: o limite de taxa e o repasse usam o IP truncado.

## Configuração

| Variável | Padrão | Uso |
|---|---|---|
| `DATABASE_URL` | obrigatória | Banco `notify` com o usuário `notify_ingest` |
| `UMAMI_URL` | obrigatória | Base do Umami na rede interna (ex.: `http://umami:3000`) |
| `LISTEN_ADDR` | `:8080` | Endereço HTTP |
| `CLIENT_IP_HEADER` | `x-notify-client-ip` | Header com o IP truncado; o serviço `umami` usa a mesma variável |
| `SCRIPT_DIR` | vazio (`/scripts` na imagem) | Pasta servida em `/s/`; vazia desliga a rota |
| `TRUSTED_PROXIES` | vazio | CIDRs ou IPs, separados por vírgula |
| `RATE_LIMIT` | `20` | Eventos por segundo por IP truncado |
| `RATE_BURST` | `40` | Rajada do token bucket |
| `SITE_CACHE_TTL` | `1m` (`5s` no Compose) | Cache das respostas da `ingest_site` |

## Desenvolvimento

De dentro de `services/`:

```bash
go test -race ./ingest-web/...
```

O teste de integração do pacote `site` roda quando `DATABASE_URL` aponta para o banco `notify`. Ele cria o usuário `notify_ingest` se faltar (precisa de superusuário; no Compose o usuário já existe) e conecta como ele com a senha `notify_ingest`, ou pela URL de `INGEST_DATABASE_URL`.

O teste ponta a ponta precisa do ambiente do Compose no ar:

```bash
docker compose up -d --build --wait
```

```bash
go test -tags e2e -count=1 ./ingest-web/e2e/...
```

Ele cria um website no Umami pela API (`admin`/`umami`), marca o site como `active` na `ingest_site`, manda um page view ao gateway (porta 3002) e confere que ele aparece no Umami. As URLs podem ser trocadas por `GATEWAY_URL`, `UMAMI_URL` e `NOTIFY_DATABASE_URL`.

A imagem (`Dockerfile`, contexto na raiz) é `scratch` com o binário e as versões publicadas do script, copiadas de `packages/tracker/published` quando a pasta existe. O healthcheck do contêiner é `ingest-web -healthcheck`.
