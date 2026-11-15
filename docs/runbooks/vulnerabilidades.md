# Runbook: resposta a vulnerabilidades

Política pública, prazos e escopo em [`SECURITY.md`](../../SECURITY.md): relato privado pela aba **Security** do GitHub (**Report a vulnerability**), confirmação em até 3 dias úteis, avaliação em 7 dias corridos, correção em 14 (crítica), 30 (alta) e 90 dias (média ou baixa), severidade por CVSS 3.1. Contexto de cadeia de suprimentos: [`docs/anti-bot-e-cadeia-de-suprimentos.md`](../anti-bot-e-cadeia-de-suprimentos.md).

## Sintomas

Um relato privado chegou; um scanner ou o Renovate apontou dependência vulnerável; um tenant diz que uma chave de ingestão vazou; suspeita de dados de um tenant vistos por outro; um operador ou uma conta foi comprometido.

## 1. Receber e avaliar

1. Confirme o recebimento no próprio relato (até 3 dias úteis). Não discuta em issue pública.
2. Reproduza num ambiente local (`docker compose up -d --wait`). Classifique pelo CVSS 3.1 e registre o prazo de `SECURITY.md`.
3. Alta prioridade, segundo o `SECURITY.md`: leitura de dados de outro tenant, burla da chave de ingestão, persistência de dado pessoal (IP completo) e inflação de métricas. Para isolamento, procure o caso em `apps/portal/test/isolation.test.ts` e em `services/collector/e2e/isolation_test.go`; a correção precisa deixar um teste novo ali.
4. Verifique se houve exploração: `audit_log` (ações `ingest_key.created`, `ingest_key.revoked`, `tenant.removed`, `operator.*`) pelo túnel (`infra/scripts/admin-tunnel.sh`):

   ```bash
   psql 'postgresql://notify:<notify_db_password>@127.0.0.1:15432/notify' \
     -c "select at, action, actor_user_id, tenant_id from audit_log order by at desc limit 50"
   ```

   (colunas conferidas em `apps/portal/src/lib/db/schema.ts`; a consulta em si não foi executada). Os logs dos serviços não têm IP nem cabeçalhos dos visitantes, então a exploração de fora costuma não deixar rastro ali.

## 2. Conter

Escolha o que se aplica; todas têm efeito imediato ou em até 1 minuto na ingestão (cache de `ingest_site` do gateway: no máximo 1 minuto; da chave no coletor: no máximo 60 s).

### Chave de ingestão vazada

O próprio tenant revoga em `/services` (a ação `revokeIngestKey` grava `ingest_key.revoked`). O coletor passa a recusar com 401. Quem cria a chave nova é o tenant; a completa só aparece na criação. Se o tenant não conseguir, o operador do SCF não revoga pela tela (as únicas escritas do operador são remover e reativar projeto): use a remoção do projeto abaixo ou, com o túnel, um `UPDATE ingest_key SET revoked_at = now() WHERE prefix = '<prefixo>'` (**não testado e fora do fluxo normal**: a linha em `audit_log` não é criada por ele; registre a operação por escrito). Revogue pelo prefixo, nunca coloque a chave completa em issue, log ou mensagem.

### Projeto (tenant) a bloquear

Entre como operador em `/operator`, abra `/operator/tenants/<tenantId>` e use a remoção do projeto (motivo obrigatório; grava `tenant.removed` no `audit_log`). Efeito: as visões `ingest_site` e `ingest_key_active` exigem tenant `active`, então o gateway passa a descartar eventos (cache de até 1 minuto) e as chaves deixam de valer; o painel do tenant some. A reativação é a mesma tela (`tenant.reactivated`). Nenhum dado é apagado.

### Operador comprometido

Pela VM ou pelo túnel, com `DATABASE_URL` do banco `notify` (como no passo 5 de `docs/deploy.md`):

```bash
DATABASE_URL='postgresql://notify:<notify_db_password>@127.0.0.1:15432/notify' \
  pnpm --filter @notify/portal operator:demote --email <e-mail>
```

Isso rebaixa a conta a usuário comum, apaga as sessões dela e grava `operator.demoted` no `audit_log`. Troque a senha da conta pelo fluxo de recuperação do portal se ela ainda for legítima. Faça depois uma revisão do `audit_log` pelas ações do operador. Se foi o acesso ao servidor que vazou, siga também [rotação de segredos](rotacao-de-segredos.md) (todos os segredos, a chave SSH e a Hetzner).

### Segredo ou acesso à infraestrutura vazado

[Rotação de segredos](rotacao-de-segredos.md). Para a VM, troque a chave SSH e, se houver dúvida, recrie a VM (`docs/deploy.md`) com segredos novos; os dados ficam no volume.

### Script de rastreamento adulterado ou com falha

Versões publicadas são imutáveis (`packages/tracker/published/vX.Y.Z/n.js`, hash em `published/sri.json`; o teste `test/published.test.ts` garante). A correção sai numa versão nova: suba a `version` do `package.json` e rode `pnpm --filter @notify/tracker release`; avise os tenants para trocar a URL e o SRI (trechos em `examples/README.md`). O gateway só descarta eventos da v1.0.0 (sem prova de JS); para forçar a saída de uma versão vulnerável, a medida disponível é retirar o arquivo de `published/` e fazer deploy de uma imagem nova do `ingest-web` (a imagem copia `packages/tracker/published` para `/scripts`; **não testado**; quebra os tenants que não migrarem).

## 3. Corrigir e publicar

1. Corrija em branch privada ou num fork de segurança do GitHub (aba **Security**, advisory privado), com teste que falhava antes.
2. PR com commit em inglês, Conventional Commits (`fix(...)`). O CI (`ci`) precisa passar.
3. Publique uma **release** (`vX.Y.Z`): o `release.yml` gera o SBOM e publica no GHCR as imagens assinadas com cosign (keyless) com atestado do SBOM.
4. Deploy: troque os digests em `infra/ansible/group_vars/all.yml` e rode `ansible-playbook site.yml -e @secrets.yml`; o playbook roda `cosign verify` com `cosign_identity_regexp` e `cosign_oidc_issuer` do `all.yml` e recusa imagem sem digest ou sem assinatura. Antes, `systemctl start notify-backup` se a versão tiver migração. Pormenores: `docs/deploy.md`, "Atualizar a aplicação".
5. Dependência vulnerável: o Renovate (`renovate.json`) trata atualizações de segurança direto; versões maiores precisam de aprovação no Dependency Dashboard. Para o Umami, ver [atualização do Umami](atualizacao-do-umami.md).

## 4. Verificar

- O teste que reproduzia o problema passa; a suíte de isolamento passa (`pnpm --filter @notify/portal test` e o job `compose` do CI).
- Passo 6 de `docs/deploy.md` na produção; a assinatura conferida no deploy (`cosign verify` no playbook).
- A medida de contenção foi desfeita só quando a correção está no ar (reativar projeto, se for o caso).

## 5. Divulgar

Depois da correção publicada, publique o advisory do GitHub e dê crédito a quem relatou, se a pessoa quiser (`SECURITY.md`). Se dados pessoais de tenants ou visitantes foram expostos, a notificação a tenants e autoridades segue a política em [`docs/privacidade.md`](../privacidade.md) (leia a seção de papéis; o procedimento de notificação de incidente de dados **não está escrito** em lugar nenhum do repositório: defina com o jurídico antes do primeiro incidente real).

## Quando escalar

- Severidade crítica ou exposição de dados entre tenants: trate como incidente aberto de imediato; prazo de correção de 14 dias corridos, na prática o mais rápido possível.
- Prazo do `SECURITY.md` em risco, ou vulnerabilidade na própria infraestrutura do provedor, no Umami, no Caddy ou no Postgres: reporte ao projeto de origem e ao suporte do provedor, e avalie desligar o componente afetado (`docker compose -f prod.yaml stop <serviço>`; parar o `ingest-web` descarta os eventos recebidos nesse intervalo).

## Não testado

Nada disto foi exercitado: nem uma revogação ou remoção em produção, nem o `operator:demote` contra o banco da VM, nem a release e o `cosign verify` contra imagens reais (ainda não há release publicada), nem a retirada de uma versão do script.
