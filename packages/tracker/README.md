# packages/tracker

Script de rastreamento em TypeScript, empacotado com esbuild. Criado na [#22](https://github.com/romulospl/notify/issues/22).

- Publicado em caminho com versão (`/s/vX.Y.Z/n.js`), imutável depois de publicado e com hash SRI.
- Envia um payload compatível com o `/api/send` do Umami, sempre pelo gateway (`services/ingest-web`), em `POST /e` na mesma origem do script.
- Sem cookies, `localStorage`, `sessionStorage` ou identificador persistente.
- Da query string só saem os parâmetros `utm_*`; o referenciador vai sem query string nem fragmento.
- Em SPA, conta cada mudança de rota (`pushState`, `replaceState` e `popstate`).
- Antes de enviar, pede um desafio em `GET /c` e resolve a prova de execução de JS com `crypto.subtle` (#25). Por isso só funciona em página HTTPS (ou `localhost`).
- Nomes de arquivo e de endpoint não podem conter termos bloqueados por ad blockers (`umami`, `analytics`, `track`, `collect`). O check `check:adblock` confere isso contra EasyList, EasyPrivacy e as listas padrão do uBlock Origin; resultado, recomendação de host e roteiro manual em [`docs/ad-blockers.md`](docs/ad-blockers.md).
- Evento customizado (v1.2.0, #50): `window.stellytics.event(nome, propriedades?)`. Nome de 1 a 50 caracteres `[A-Za-z0-9_.:-]`; até 10 propriedades com valor string (até 100 bytes), número ou booleano. Fora disso o evento é ignorado em silêncio. **Nunca** mande endereço de carteira, hash de transação, valor ou dado pessoal.

```js
window.stellytics?.event("wallet_connected", { wallet: "freighter" });
```

- Stellar Wallets Kit (v1.4.0, **opt-in**, #50): `window.stellytics.wallets(kit, KitEventType)`. **Não é automático**: o tenant precisa chamar uma vez, passando o kit e o enum, porque o script não alcança o módulo do kit sozinho ([ADR 0010](../../docs/adr/0010-adaptador-opt-in-do-wallets-kit.md)). Assina só `WALLET_SELECTED` (`wallet_selected` com `{ wallet: id }`) e `DISCONNECT` (`wallet_disconnected`); nunca `STATE_UPDATED`, endereço, chave pública, `networkPassphrase`, hash ou valor. Idempotente por kit, devolve a função que cancela e nunca lança.

```js
window.stellytics?.wallets(StellarWalletsKit, KitEventType);
```

## Instalação no site do tenant

```html
<script
  defer
  src="https://<gateway>/s/v1.1.0/n.js"
  integrity="sha384-oJV5jTaVFhvi5Y8R/QGZrzfVps1szxTK74tBPsk7LIsuUoUzzFetJ//8xd0NUqs0"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

O hash de cada versão está em [`published/sri.json`](published/sri.json). O script nunca se atualiza sozinho: para mudar de versão, o tenant troca o caminho e o `integrity` no próprio HTML.

## Desenvolvimento

```bash
pnpm --filter @stellytics/tracker test       # vitest (happy-dom)
pnpm --filter @stellytics/tracker build      # gera dist/n.js
pnpm --filter @stellytics/tracker release    # publica a versão do package.json em published/
pnpm --filter @stellytics/tracker check:adblock  # testa script e endpoints contra as listas de ad blockers
```

Para publicar uma versão nova:

1. Mude o código.
2. Suba a `version` no `package.json` (semver).
3. Rode o `release` e commite `published/vX.Y.Z/n.js` e `published/sri.json`.

Versões antigas nunca são apagadas nem alteradas. A v1.0.0 não manda a prova de execução de JS, então o gateway descarta os eventos dela desde a #25: use a v1.1.0 ou mais nova. O teste `test/published.test.ts` confere os hashes e falha se o código atual não for o da versão do `package.json`.
