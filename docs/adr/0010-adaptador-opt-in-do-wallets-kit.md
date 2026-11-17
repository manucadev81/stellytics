# 0010. Adaptador opt-in do Stellar Wallets Kit no script

- **Status:** Aceito
- **Data:** 15/11/2026
- **Issue:** [#50](https://github.com/romulospl/notify/issues/50) (continua aberta: refs, não fecha)

## Contexto

O [ADR 0008](0008-eventos-customizados-e-wallets-kit.md) entregou `window.stellytics.event` e deixou a captura dos eventos do Stellar Wallets Kit pendente, listando como alternativa um adaptador em que o tenant passa o kit ao script. Este ADR registra essa alternativa implementada e corrige um erro do 0008.

Fatos reconfirmados em 15/11/2026, na página oficial https://stellarwalletskit.dev/how-to/kit-events.html (lida por fetch nesta data) As páginas do Stellar (`/docs/tools/developer-tools/wallets`, via MCP stellar-raven) são só um ponteiro para o kit e **não corroboram** a forma do evento: todos os fatos abaixo vêm de stellarwalletskit.dev, uma única fonte.

Fatos:

- `StellarWalletsKit.on(KitEventType.X, handler)` devolve a função de cancelamento (`const sub = ...on(...)`; "To unsubscribe from the updates, do this: `sub()`").
- `KitEventType` tem `STATE_UPDATED = "STATE_UPDATE"` (o valor do texto não é o nome do membro), `WALLET_SELECTED = "WALLET_SELECTED"` e `DISCONNECT = "DISCONNECT"`.
- **O handler recebe `{ eventType, payload }`**: `WALLET_SELECTED` tem `payload: { id: string | undefined }`, `DISCONNECT` tem `payload` vazio e `STATE_UPDATED` tem `payload: { address, networkPassphrase }`. O exemplo da página lê `event.payload.address`.
- **Correção ao ADR 0008:** o trecho manual do guia de onboarding (`e.id`) estava errado; o certo é `e.payload.id`. O ADR 0008 só descrevia os campos sem o envelope `payload`, e o guia o copiou do jeito errado. O guia foi corrigido junto com este ADR (o snippet manual foi substituído por `stellytics.wallets`). O 0008 não é reescrito.
- **Não verificado:** se `on` é síncrono (a página não diz; o adaptador só aproveita o retorno se for função); se a `id` pode ter outras formas além de uma string de módulo (a página só diz `string | undefined`); se o kit expõe referência em `window`; comportamento do código-fonte do kit (não lido). Por isso o adaptador lê `event.payload.id` e valida o formato, e ignora o que não encaixar.

## Decisão

1. **O script ganha `window.stellytics.wallets(kit, KitEventType)` (v1.3.0).** O tenant chama **uma vez**, depois de iniciar o kit, passando o objeto do kit e o enum.
2. **Isto NÃO é captura automática nem "sem configuração".** O critério original da #50 ("capturados automaticamente, sem configuração do tenant") continua **não atendido**: sem a chamada do tenant, nada é capturado, pelos motivos do ADR 0008 (o script é um bundle separado e não alcança o módulo do kit). A #50 não fecha por este ADR.
3. **Só dois eventos são assinados:** `WALLET_SELECTED` envia `wallet_selected` com `{ wallet: id }`, onde `id` é o ID do módulo (por exemplo `freighter`) e passa pelo mesmo charset das chaves do gateway (`[A-Za-z0-9][A-Za-z0-9_.-]{0,31}`, no máximo 32 caracteres: rejeita endereço Stellar de 56, hash de 64, hex de 40 caracteres no formato EVM e texto com espaço); `DISCONNECT` envia `wallet_disconnected` sem dados. `STATE_UPDATED` **nunca** é assinado, e nada de endereço, chave pública, `networkPassphrase`, hash ou valor sai do navegador. Id ausente ou inválido: o evento não é enviado.
4. **Comportamento defensivo:** idempotente por kit (guardado em `WeakMap`; segunda chamada devolve a mesma função de cancelar); kit ou enum inválidos, `on` que lança ou handler que lança nunca propagam (try/catch silencioso); se a segunda assinatura falhar, a primeira é desfeita; sem cookie nem storage; devolve a função que cancela as duas assinaturas (e é idempotente). O gateway não mudou: os eventos passam pelas mesmas regras de `name`/`data` do ADR 0008.
5. Os nomes (`wallets`, `wallet_selected`, `wallet_disconnected`) não têm termos bloqueados por ad blockers.

**Limitação:** o limite de 32 caracteres é uma heurística, não uma prova. Um valor curto qualquer que o kit devolva como `id` seria aceito; os IDs reais dos módulos do kit são curtos e genéricos (por exemplo `freighter`), mas não foi verificada a lista completa.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Adaptador opt-in `stellytics.wallets` (escolhida) | Usa a API pública `on`, simples, sem monkey patching | Exige uma linha do tenant; não cumpre "sem configuração" |
| Monkey patching do kit | Sem configuração | Ver ADR 0008 |
| Assinar `STATE_UPDATED` também | Mais sinal (rede) | Traz o endereço; rejeitado como regra dura de privacidade |
| Manter só o snippet manual de 3 linhas | Nada novo no script | Cada tenant reescreve a lógica (e o guia tinha o erro `e.id`) |

## Consequências

- Nova versão imutável `v1.3.0` (a v1.0.0 a v1.2.0 seguem intactas).
- O guia de onboarding documenta a chamada e o aviso de nunca enviar endereço.
- Fica pendente: confirmar no código do kit se `on` é síncrono e outros formatos de `id`; a captura sem configuração do tenant segue sem solução aceitável.
