# 0007. Cadastro com aprovação prévia

- **Status:** Aceito
- **Data:** 15/11/2026
- **Issue:** #49

## Contexto

O cadastro é aberto (#17, #60): quem confirma o e-mail cria um projeto `active` e já pode enviar eventos. Para os marcos seguintes o SCF pode querer controlar quem entra, por exemplo para limitar a plataforma a projetos do ecossistema Stellar. A issue #49 pede um modo configurável em que projetos novos ficam `pending` até um operador aprovar, e uma tela de aprovação.

O enum `tenant_status` já tem o valor `pending` desde a #16, e as visões `ingest_site` e `ingest_key_active` (migração 0002) só aceitam tenant `active`. O spec do E2 (seção 6) previa `pending` com painéis vazios e cadastro liberado, mas marcava o estado como "não usado no MVP".

## Decisão

- **Estado `pending` no enum existente, sem coluna nova e sem migração.** O ciclo é `pending` → `active` (aprovado) ou `pending` → `removed` (rejeitado).
- **Modo por variável de ambiente `SIGNUP_APPROVAL`** (`true` ou `1` liga; desligado por padrão), lida por `src/lib/env.ts` e usada só em `createProject`. Desligado, o comportamento é o de antes, byte a byte (teste de regressão em `test/onboarding.test.ts`). No Ansible é `signup_approval` em `group_vars/all.yml`.
- **O que o projeto pendente vê:** só uma tela de espera em `/dashboard`. `requireTenantContext()` redireciona para ela em qualquer outra página ou ação do tenant (a tela pede `allowPending`), e a camada de dados também recusa criar site, serviço e chave. Isto muda o que o spec do E2 dizia (pendente com painéis vazios e cadastro liberado); o spec foi atualizado.
- **Ingestão:** nenhuma mudança. As visões já tratam `pending` como não ativo, então o gateway e o coletor recusam, e a aprovação vale em até 1 minuto (o cache deles).
- **Aprovar e rejeitar:** funções `approveTenant` e `rejectTenant` em `src/lib/data/operator/tenants.ts`, que reutilizam a transação com `SELECT ... FOR UPDATE` da remoção. Só valem para `pending` (de outro status nada é gravado). Cada uma grava uma linha no `audit_log` (`tenant.approved`, `tenant.rejected`) com quem, quando, o status anterior e o motivo. O motivo é obrigatório na rejeição, porque o projeto o vê; na aprovação é opcional.
- **Rejeitar é remover:** o projeto rejeitado fica `removed` com o motivo, vê o aviso de projeto removido e o operador pode reativá-lo (D6).
- **Tela:** `/operator/approvals` lista os pendentes, do mais antigo para o mais novo, com aprovar e rejeitar em cada um. O detalhe do projeto (`/operator/tenants/[tenantId]`) mostra os mesmos botões quando o status é `pending`, e `/operator` avisa quando há pendentes.
- **Sem e-mail de notificação nesta issue.** O projeto descobre a aprovação ao entrar de novo.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Novo status `pending` no enum (escolhida) | O valor já existe; visões, painéis do SCF e listagens já o tratam como não ativo; sem migração | Quem lê o status precisa lembrar que `pending` não é `removed` |
| Coluna `approved_at` ou booleano à parte | Separa aprovação de remoção | Dois campos para o mesmo conceito; as visões de ingestão teriam de mudar, e esquecer uma deixaria um tenant não aprovado enviando dados |
| Configuração no banco, alterada por tela do operador | Liga e desliga sem reiniciar | Mais uma escrita e uma tela de administração (o spec do E2, D3, evita isso); a escolha é de quem opera o servidor |
| Pendente com painéis vazios e cadastro liberado (spec original) | Menos código no portal | O pendente já cadastraria sites no Umami e geraria chaves antes de qualquer aprovação, e aprovaria-se depois de o tenant ter dado trabalho |
| Rejeitar apagando o tenant e seus dados | Limpa o banco | Perde o histórico e o motivo; some do `audit_log`, que referencia o tenant; irreversível. Remover é desativação (spec do E2, seção 6) |
| Estado `rejected` próprio | Distingue rejeitado de removido | Mexe no enum e em toda checagem de `removed`; o `audit_log` já distingue (`tenant.rejected`) |
| E-mail ao operador e ao projeto | Ninguém precisa abrir a tela | Exige destinatário configurado e mais testes de e-mail; fica para depois |

## Consequências

- Com o modo desligado nada muda; ligar afeta só cadastros novos. Projetos já `active` seguem ativos, e os `pending` continuam na fila se o modo for desligado depois (o operador ainda os resolve).
- O portal passa a ter dois lugares que tratam `pending` (`requireTenantContext` e as funções de escrita de `data/`), de modo que uma página nova do tenant já nasce coberta.
- A mudança de modo exige reiniciar o portal (variável de ambiente).
- Sem notificação, o SCF precisa olhar `/operator/approvals` (o `/operator` mostra a contagem). Um e-mail é a evolução natural, mas fica fora desta issue.
