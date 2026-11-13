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

## Privacidade (#24)

- **IP:** o Umami recebe só o IP truncado (último octeto do IPv4 zerado, IPv6 cortado em /48) e geolocaliza a partir dele. O IP completo não vai para banco, log nem cache.
- **Logs:** nenhum registro leva IP, `X-Forwarded-For` ou `User-Agent`. Um panic é tratado pelo próprio gateway, porque o `net/http` o registraria com o endereço do cliente.
- **Cookies:** nenhuma resposta grava cookie, e o `Cookie` que chega não é repassado ao Umami. O script não usa cookie nem storage (ver `packages/tracker`).
- **Dado pessoal:** o Umami guarda navegador, sistema, tipo de aparelho, tela, idioma e país, região e cidade do IP truncado. Nada disso identifica uma pessoa, então não há dado sujeito a apagamento.
- **Testes:** `privacy_test.go` (logs e cookies) e `e2e/privacy_test.go`. Este último manda IPs conhecidos pelo `X-Forwarded-For` e confere duas coisas: que o país da sessão no Umami saiu do IP truncado, e que nenhuma coluna de texto dos bancos `umami` e `notify` contém o IP completo.

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

O teste de integração do pacote `site` roda quando `DATABASE_URL` aponta para o banco `notify`. Ele precisa dos usuários de `infra/compose/postgres/init/02-ingest-roles.sh` e aplica as migrações do portal quando a visão `ingest_site` não existe. Depois conecta como `notify_ingest`, com a senha `notify_ingest` ou pela URL de `INGEST_DATABASE_URL`.

O teste ponta a ponta precisa do ambiente do Compose no ar:

```bash
docker compose up -d --build --wait
```

```bash
go test -tags e2e -count=1 ./ingest-web/e2e/...
```

Ele cria um website no Umami pela API (`admin`/`umami`) e um tenant ativo com esse site no banco `notify`. Depois manda page views ao gateway (porta 3002) e confere o que chega ao Umami. O teste de privacidade precisa que o gateway confie no `X-Forwarded-For` do teste: suba o Compose com `INGEST_TRUSTED_PROXIES=172.16.0.0/12,192.168.0.0/16,10.0.0.0/8`, como no CI. As URLs podem ser trocadas por `GATEWAY_URL`, `UMAMI_URL`, `NOTIFY_DATABASE_URL` e `UMAMI_DATABASE_URL`.

A imagem (`Dockerfile`, contexto na raiz) é `scratch` com o binário e as versões publicadas do script, copiadas de `packages/tracker/published` quando a pasta existe. O healthcheck do contêiner é `ingest-web -healthcheck`.
