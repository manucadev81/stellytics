# examples

Integrações de referência, usadas também nos testes de ponta a ponta. Cada exemplo é um pacote próprio do workspace (`examples/<nome>`).

| Pasta | O que mostra |
|---|---|
| [`static-html/`](static-html/) | Site estático: cada página carregada é um page view |
| [`spa/`](spa/) | SPA sem framework, com `history.pushState`: cada mudança de rota é um page view, sem recarregar |
| [`nextjs-ssr/`](nextjs-ssr/) | Next.js 16 (App Router) com SSR: a tag sai no HTML do servidor, e a navegação pelo `<Link>` conta como page view |
| [`api-backend/`](api-backend/) | Backend HTTP com OpenTelemetry e guia de dual-export (SDK e OTel Collector) para o coletor do notify (#35) |
| [`e2e/`](e2e/) | Playwright: abre os três num Chromium e confere os page views no banco do Umami (#27) |

## Instalação do script (trechos para o guia de onboarding)

O script fica no gateway, com versão fixa no caminho e hash SRI. Troque `<gateway>` pelo endereço do gateway e `<website-id>` pelo ID do site mostrado no portal. Os hashes de cada versão estão em [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json).

### HTML estático e SPA

No `<head>` de cada página (numa SPA, só no `index.html`):

```html
<script
  defer
  src="https://<gateway>/s/v1.1.0/n.js"
  integrity="sha384-oJV5jTaVFhvi5Y8R/QGZrzfVps1szxTK74tBPsk7LIsuUoUzzFetJ//8xd0NUqs0"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

Numa SPA não precisa de mais nada: o script conta as mudanças de rota feitas por `history.pushState`, `history.replaceState` e pelo voltar do navegador. Roteadores como React Router, Vue Router e o do SvelteKit usam essas APIs.

### Next.js (App Router)

No `app/layout.tsx`, dentro do `<head>`:

```tsx
<head>
  <script
    defer
    src="https://<gateway>/s/v1.1.0/n.js"
    integrity="sha384-oJV5jTaVFhvi5Y8R/QGZrzfVps1szxTK74tBPsk7LIsuUoUzzFetJ//8xd0NUqs0"
    crossOrigin="anonymous"
    data-site="<website-id>"
  />
</head>
```

A tag vai no HTML que o servidor gera, e a navegação pelo `<Link>` conta como page view. Não use o `next/script` com `strategy="lazyOnload"`: ele carrega o script tarde, e o primeiro page view pode se perder.

### Requisitos

- **HTTPS:** a prova de execução de JS usa `crypto.subtle`, que o navegador só oferece em HTTPS ou `localhost`. Numa página HTTP comum, o script não envia eventos.
- **Sem cookie nem consentimento:** o script não grava cookie nem storage, e o IP é truncado no gateway.
- **Para trocar de versão,** mude o caminho e o `integrity` no próprio HTML. O script nunca se atualiza sozinho.

## Rodar localmente

Com o Compose no ar (gateway na porta 3002) e um site cadastrado no portal (ou criado à mão no Umami e na `ingest_site`):

```bash
cd examples/static-html && NOTIFY_WEBSITE_ID=<id> PORT=4001 node ../serve.mjs .
cd examples/spa && NOTIFY_WEBSITE_ID=<id> PORT=4002 node ../serve.mjs . spa
cd examples/nextjs-ssr && pnpm build && NOTIFY_WEBSITE_ID=<id> pnpm start
```

O `serve.mjs` troca o comentário `<!-- notify:script -->` das páginas pela tag do script. O site vem de `NOTIFY_WEBSITE_ID`, o gateway de `NOTIFY_GATEWAY_URL` (padrão `http://127.0.0.1:3002`) e a versão de `NOTIFY_SCRIPT_VERSION` (padrão: a mais nova publicada). O exemplo Next.js lê as mesmas variáveis.

## Ponta a ponta

```bash
pnpm --filter @notify/examples-e2e exec playwright install chromium
pnpm --filter @notify/examples-e2e e2e
```

O `run.mjs` cria um website no Umami para cada exemplo e um tenant com os três sites no banco `notify`. Depois roda o Playwright, que sobe os exemplos nas portas 4001 a 4003, e no fim apaga tudo.

Os testes conferem:
- **HTML estático:** um page view por página;
- **SPA:** quatro page views, incluindo o voltar do navegador, sem recarregar a página;
- **Next.js:** a tag com `integrity` no HTML do servidor e a navegação pelo `<Link>`.

As URLs podem ser trocadas por `UMAMI_URL`, `NOTIFY_DATABASE_URL`, `UMAMI_DATABASE_URL` e `NOTIFY_GATEWAY_URL`. No CI, o teste roda no job `compose`.
