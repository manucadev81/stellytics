# packages/tracker

Script de rastreamento em TypeScript, empacotado com esbuild. Criado na [#22](https://github.com/romulospl/notify/issues/22).

- Publicado em caminho com versão (`/s/vX.Y.Z/n.js`), imutável depois de publicado e com hash SRI.
- Envia um payload compatível com o `/api/send` do Umami, sempre pelo gateway (`services/ingest-web`), em `POST /e` na mesma origem do script.
- Sem cookies, `localStorage`, `sessionStorage` ou identificador persistente.
- Da query string só saem os parâmetros `utm_*`; o referenciador vai sem query string nem fragmento.
- Em SPA, conta cada mudança de rota (`pushState`, `replaceState` e `popstate`).
- Nomes de arquivo e de endpoint não podem conter termos bloqueados por ad blockers (`umami`, `analytics`, `track`, `collect`).

## Instalação no site do tenant

```html
<script
  defer
  src="https://<gateway>/s/v1.0.0/n.js"
  integrity="sha384-rDheakAwCux4hWanUJ6JyJpdkBlz8lGOufF+s1QHaJZ1UOfk7+m81X6oFWw/I568"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

O hash de cada versão está em [`published/sri.json`](published/sri.json). O script nunca se atualiza sozinho: para mudar de versão, o tenant troca o caminho e o `integrity` no próprio HTML.

## Desenvolvimento

```bash
pnpm --filter @notify/tracker test       # vitest (happy-dom)
pnpm --filter @notify/tracker build      # gera dist/n.js
pnpm --filter @notify/tracker release    # publica a versão do package.json em published/
```

Para publicar uma versão nova:

1. Mude o código.
2. Suba a `version` no `package.json` (semver).
3. Rode o `release` e commite `published/vX.Y.Z/n.js` e `published/sri.json`.

Versões antigas nunca são apagadas nem alteradas. O teste `test/published.test.ts` confere os hashes e falha se o código atual não for o da versão do `package.json`.
