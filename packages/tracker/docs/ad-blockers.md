# Script e endpoints contra ad blockers (#23)

O script e os endpoints do gateway não podem ser bloqueados pelas listas que vêm ligadas por padrão no uBlock Origin. Esta página registra como isso é testado, o último resultado e o roteiro para conferir à mão no navegador.

## O que é testado

Três requisições, feitas pela página de um tenant:

| Requisição | Tipo | Quem faz |
| --- | --- | --- |
| `GET https://<gateway>/s/vX.Y.Z/n.js` (todas as versões de `published/sri.json`) | `script` | a tag `<script>` do tenant |
| `POST https://<gateway>/e` | `xmlhttprequest` (`fetch`) | o script, a cada evento |
| `GET https://<gateway>/c?s=<website-id>` | `xmlhttprequest` (`fetch`) | o script, para pegar o desafio da prova de JS |

Cada uma é avaliada a partir de uma página de terceiro (`www.tenant-site.example.net`, o caso comum) e de uma página no mesmo domínio do gateway (`www.example.com`).

## Listas

As listas ligadas por padrão no uBlock Origin, baixadas na hora em cada execução (nada é commitado):

- EasyList — `https://easylist.to/easylist/easylist.txt`
- EasyPrivacy — `https://easylist.to/easylist/easyprivacy.txt`
- uBlock filters (com os `!#include`), privacy, badware, quick-fixes e unbreak — `https://ublockorigin.github.io/uAssets/filters/*.txt`
- Peter Lowe's Ad and tracking server list — `https://pgl.yoyo.org/adservers/`

A Online Malicious URL Blocklist, também ligada por padrão, fica de fora: só tem domínios de malware.

## Como roda

```bash
pnpm --filter @notify/tracker check:adblock                    # host de exemplo gw.example.com
pnpm --filter @notify/tracker check:adblock e.seu-dominio.org  # também confere um host real
```

O script (`scripts/check-adblock.ts`) usa o matcher de regras de rede do [`@ghostery/adblocker`](https://github.com/ghostery/adblocker) (MPL-2.0), que entende a sintaxe do uBlock Origin e da Adblock Plus. Só as regras de rede importam aqui; as cosméticas escondem elementos da página e não afetam requisições.

O veredito vem de todas as listas juntas, como no navegador (uma exceção de uma lista vale para as outras); quando algo é bloqueado, a saída mostra a regra e a lista de origem. Antes de valer, o resultado passa por controles que precisam ser bloqueados (`/umami.js` e `/api/collect` no host do gateway e o `analytics.js` do Google). Se algum controle passar, o matcher ou as listas quebraram e o check falha. Ele também falha se uma lista vier vazia ou não baixar.

O job `adblock` do CI roda o check em todo PR e push na `main`. Como as listas mudam todo dia, uma falha nesse job sem mudança no código quer dizer que uma regra nova pegou o script ou um endpoint: veja a regra na saída e decida entre pedir a correção na lista ou trocar o nome.

## Resultado

Execução de 14/11/2026 (listas: EasyList 202611140151, EasyPrivacy 202611140158, uBlock filters de 09/11/2026, privacy, badware e quick-fixes de 13/11/2026, unbreak de 06/11/2026), matcher `@ghostery/adblocker` 2.20.3:

- **Nenhuma das três requisições é bloqueada** em `gw.example.com`, nas duas versões publicadas (v1.0.0 e v1.1.0), de página de terceiro ou do mesmo domínio.
- Os caminhos padrão do Umami são bloqueados no mesmo host: `/umami.js` pela regra `/umami.js` e `/api/collect` pela regra `/api/collect|$ping,xhr`. Por isso o gateway usa `/s/`, `/e` e `/c`, e repassa ao `/api/send` do Umami só do lado do servidor.
- Hoje nenhuma lista padrão bloqueia pelo nome do subdomínio: `analytics.`, `track.`, `tracking.`, `collect.`, `umami.`, `stats.`, `metrics.`, `telemetry.`, `insights.`, `events.`, `ingest.` e `notify.` em `example.com` passam.

## Recomendação de host

Use um subdomínio neutro e curto para o gateway, sem `analytics`, `track`, `tracking`, `collect`, `umami`, `stats`, `metrics` ou `telemetry` (por exemplo `e.<domínio>` ou `gw.<domínio>`). As listas padrão não bloqueiam esses nomes hoje, mas:

- listas que muita gente liga além das padrão (AdGuard Tracking Protection, filtros de DNS como os do Pi-hole e NextDNS) não foram testadas aqui e podem bloquear por padrão de hostname, e esses termos seriam os primeiros alvos;
- uma regra genérica nova nas listas padrão pegaria todos os tenants de uma vez, e trocar o host exige mudar o HTML de cada site.

O mesmo vale para o domínio registrável: ele entra nas listas assim que for associado a rastreamento, independentemente do subdomínio. Ao escolher o host de produção, rode o check com ele como argumento.

## Roteiro manual com uBlock Origin

Para conferir no navegador, com o ambiente local (`docker compose up -d --wait`) ou com o gateway de produção:

1. Instale o uBlock Origin num perfil limpo do Firefox ou do Chrome e deixe as listas padrão (no painel, aba "Filter lists", clique em "Update now").
2. Cadastre um site no portal e copie o snippet de instalação.
3. Sirva uma página HTML com o snippet num host diferente do gateway (por exemplo `python3 -m http.server 8080` e abra `http://localhost:8080`; a prova de JS exige HTTPS ou `localhost`).
4. Abra o logger do uBlock Origin (ícone do uBO → ícone de lista, "Open the logger") e recarregue a página.
5. Confira que `n.js`, `/c?s=...` e `/e` aparecem sem fundo vermelho (bloqueado) e, na aba Network das ferramentas de desenvolvedor, que `/e` respondeu `204`.
6. Confira que o pageview aparece no painel do site no portal.

Anote a data, o navegador e a versão do uBlock Origin ao registrar o resultado aqui.
