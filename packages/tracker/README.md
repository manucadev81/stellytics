# packages/tracker

Script de rastreamento em TypeScript, empacotado com esbuild.

- Publicado em caminho com versão (`/s/vX.Y.Z/<nome>.js`), imutável depois de publicado e com hash SRI.
- Envia a prova de execução de JS e um payload compatível com o `/api/send` do Umami, sempre pelo gateway.
- Sem cookies, `localStorage` ou identificador persistente.
- Nomes de arquivo e de endpoint não podem conter termos bloqueados por ad blockers (`umami`, `analytics`, `track`, `collect`).

Criado na [#22](https://github.com/romulospl/notify/issues/22).
