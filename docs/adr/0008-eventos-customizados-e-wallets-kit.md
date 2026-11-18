# 0008. Eventos customizados no script e captura do Stellar Wallets Kit

- **Status:** Aceito
- **Data:** 15/11/2026
- **Issue:** [#50](https://github.com/romulospl/stellytics/issues/50)

## Contexto

A #50 pede duas coisas: uma API de evento customizado no script e a captura automática dos eventos do Stellar Wallets Kit, sem configuração do tenant. O Umami já aceita evento customizado em `/api/send` (campos `name` e `data`), mas o gateway remonta o payload só com `website`, `hostname`, `url`, `referrer`, `title`, `language` e `screen`, e descarta o resto de propósito.

Fatos levantados sobre o Wallets Kit (consulta em 15/11/2026):

- A página oficial de ferramentas de carteira lista o kit e aponta para a documentação em stellarwalletskit.dev (stellarDocs, `/docs/tools/developer-tools/wallets`, lida pelo MCP stellar-raven em 15/11/2026). Ela não descreve eventos.
- A página "Kit events" da documentação do kit (stellarwalletskit.dev/how-to/kit-events.html, lida em 15/11/2026) diz que se assina com `StellarWalletsKit.on(KitEventType.STATE_UPDATED, handler)`, que a chamada devolve uma função de cancelamento e que há três tipos: `STATE_UPDATED` (`address`, `networkPassphrase`), `WALLET_SELECTED` (`id`) e `DISCONNECT` (vazio).
- A skill `dapp` (usada via Raven, nota de julho de 2026) descreve a v2 como um singleton estático iniciado pelo app (`StellarWalletsKit.init(...)`, `authModal()`, `signTransaction(...)`), distribuído no JSR.
- **Não verificado:** se os eventos são callbacks em processo ou `CustomEvent` no DOM (a página não diz); se o kit expõe alguma referência global em `window`; se existe evento de assinatura (a página lista só os três acima). A leitura do código-fonte no GitHub não foi possível (a URL tentada deu 404).

## Decisão

1. **Evento customizado: entregue.** O script expõe `window.stellytics.event(nome, propriedades?)`. O nome não contém termos bloqueados por ad blockers. O script valida no navegador e o gateway valida de novo (a regra vale no gateway): chave com letra ou dígito no começo (recusa `__proto__`); número finito com literal de até 32 caracteres; nome com 1 a 50 caracteres `[A-Za-z0-9_.:-]` começando por letra ou dígito; até 10 propriedades; chave de até 40 caracteres `[A-Za-z0-9_.-]`; valor string (até 100 bytes), número ou booleano (aninhado, nulo e lista são recusados com `400`, `data` sem `name` também; esta última regra existe só no gateway, o script não a confere porque o próprio `event()` sempre manda `name`). Só `name` e `data` validados são repassados ao Umami; `id`, `ip`, `userAgent` e `timestamp` continuam descartados, e a ordem no gateway não muda (prova de JS, `ingest_site`, fingerprints, repasse). A API é a versão 1.2.0 do script; as versões antigas continuam intactas.
2. **Wallets Kit: não entregue.** O kit só avisa por callbacks registrados na instância (estática) do próprio módulo do tenant. O script de rastreamento é um bundle separado, carregado por `<script>`, e não alcança esse módulo sem (a) o tenant chamar algo, o que é configuração do tenant e contraria o critério, ou (b) interceptar o kit por monkey patching (`StellarWalletsKit.on`/`authModal`/`signTransaction`), o que depende de o kit estar em `window` (não verificado), quebra com minificação e com a ordem de carregamento, e muda o comportamento de código de terceiros dentro da página do tenant. Isso é frágil demais para um script imutável por versão e com SRI.
3. **Privacidade é restrição dura.** Mesmo com um adaptador opcional, `STATE_UPDATED` traz `address` e `DISCONNECT`/`WALLET_SELECTED` trazem o ID do módulo. Endereço de carteira, chave pública, hash de transação e valores **nunca** são capturados nem aceitos como convenção. Se um adaptador voltar a ser proposto, ele manda só o nome do evento (`wallet_selected`, `wallet_disconnected`) e o ID genérico da carteira (por exemplo `freighter`), nunca o endereço.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Interceptar o kit por monkey patching no script | Zero configuração do tenant | Depende de global não verificado, quebra com bundlers e ordem de carga, altera código de terceiros, difícil de testar |
| Adaptador opcional: o tenant passa o kit ao script (`stellytics.walletKit(StellarWalletsKit, KitEventType)`) | Robusto, usa a API pública `on` | É configuração do tenant (não atende "sem configuração"); precisa do script ou de um pacote npm; fica como pendência |
| Observar o DOM do modal do kit | Sem acoplamento ao módulo | Depende de seletores internos, frágil, e não vê assinatura |
| Aceitar `data` livre sem limites | Simples | Abre espaço para dado pessoal e para estouro de armazenamento no Umami |
| Só números e booleanos em `data` | Menos risco de dado pessoal | Impede o caso comum de rótulo (`plan: "pro"`) |

## Consequências

- Tenants já registram eventos próprios, por exemplo `stellytics.event("wallet_connected", { wallet: "freighter" })`, chamando a API no ponto do código onde conectam a carteira. O guia de onboarding avisa para não pôr endereço, hash ou valor.
- O critério "eventos do Wallets Kit capturados automaticamente" fica **pendente**. Antes de retomar: confirmar no código do kit o mecanismo de eventos e se há referência global; decidir entre o adaptador opcional e nenhuma captura.
- Valores de string em `data` ainda são texto livre do tenant: a responsabilidade de não enviar dado pessoal é dele (o gateway só limita tamanho e tipo).
- Listar os eventos no painel web do tenant fica como pendência: o painel não lê hoje `/metrics` do Umami com `type=event`, e isso não foi verificado nesta issue.
