# Runbook: rotação de segredos

A ordem exata para trocar um segredo (editar o `secrets.yml`, rodar o playbook, atualizar a senha no banco, reiniciar) está em [`docs/deploy.md`](../deploy.md), seção "Atualizar a aplicação", bloco **Trocar um segredo**. Siga-o; **não** trocar só o `secrets.yml`: os usuários do banco guardam a senha antiga. Este runbook diz o que cada segredo afeta e como conferir.

## Quando rotacionar

- Suspeita ou confirmação de vazamento (log, repositório, máquina de quem administra, ex-colaborador): rotacione na hora, antes de investigar.
- Saída de alguém com acesso ao `secrets.yml` ou à VM.
- Rotina: sem calendário definido no repositório; defina um (por exemplo, anual) e registre aqui.

## Os segredos e o efeito de cada troca

Todos listados em `infra/ansible/secrets.example.yml`.

| Segredo | Efeito da troca | Passo extra além do `docs/deploy.md` |
|---|---|---|
| `postgres_password`, `notify_db_password`, `umami_db_password` | O Postgres é recriado; portal, Umami e gateway ficam sem banco até o `ALTER ROLE` | `ALTER ROLE ... PASSWORD` e `restart portal umami ingest-web` (no guia). Os eventos web do intervalo são descartados: faça fora do pico |
| `notify_ingest_db_password`, `notify_collector_db_password` | Idem para o gateway | Reaplicar `02-ingest-roles.sh` (idempotente), como no guia |
| `portal_auth_secret` | Todas as sessões do portal caem | Avise os usuários |
| `umami_app_secret` | As sessões do Umami caem; a chave de API pode deixar de valer | Se o portal receber 401 do Umami, gere nova com `pnpm --filter @notify/portal umami:service-account` (túnel aberto, senha de admin do Umami em `UMAMI_ADMIN_PASSWORD`) e ponha em `umami_api_key` |
| `umami_api_key` | O portal usa a nova chave ao ser recriado | Rode o playbook de novo; revogue a antiga na interface do Umami (pelo túnel) |
| Senha de `admin` do Umami | Só vale pela interface, pelo túnel | Troque pela interface; ela nunca fica no `secrets.yml` |
| `ingest_proof_secret` | Os desafios em circulação perdem a validade; os navegadores pedem um novo em `GET /c` no próximo evento | Nenhuma ação (mínimo de 32 caracteres) |
| `smtp_url` | Confirmação de e-mail e recuperação de senha dependem dele | Teste com um cadastro ou `/forgot-password` depois |
| `walg_s3_access_key`, `walg_s3_secret_key` | O arquivamento de WAL e o backup usam a nova credencial ao recriar o Postgres | Crie a credencial nova na Hetzner antes, rode o playbook e confirme `backup-list`; só então revogue a antiga ([backup e restauração](backup-e-restauracao.md)) |
| `walg_libsodium_key` | **Perigoso**: backups antigos continuam cifrados com a chave velha | Veja "Chave de cifra" em [backup e restauração](backup-e-restauracao.md); não troque sem guardar a antiga |
| `monitor_smtp_password` | Alertas por e-mail | `ansible-playbook monitoring.yml -e @secrets.yml` e teste de alerta (ver `docs/deploy.md`, "Monitoramento") |
| `HCLOUD_TOKEN` e credenciais do Object Storage do OpenTofu | Só valem na sua máquina | Revogue no console da Hetzner e crie outras |
| Chave SSH de administração | Acesso à VM | Troque `admin_ssh_public_key` no OpenTofu e reaplique; confira antes que a nova entra |
| Chaves de ingestão dos tenants (`ntf_...`) | Ficam com o tenant | [Vulnerabilidades: chave de ingestão vazada](vulnerabilidades.md#chave-de-ingestão-vazada) |

## Diagnóstico depois da troca

```bash
cd /opt/notify/infra/compose
docker compose -f prod.yaml ps
docker compose -f prod.yaml logs --tail 100 portal ingest-web umami
```

Erros de autenticação de banco (`password authentication failed`) indicam que o `ALTER ROLE` ou o `02-ingest-roles.sh` não rodou ou usou outro valor.

## Verificação

Passo 6 de `docs/deploy.md` (curls de fora e `docker compose ps`), entrar no portal, abrir o painel de um site, receber um e-mail de confirmação, e `wal-g backup-list` se a credencial do backup mudou.

## Quando escalar

- O Postgres não volta com a senha nova: restaure o `secrets.yml` anterior (guarde cópia antes) e repita com calma.
- Credencial vazada que dá acesso ao bucket de backup: trate como incidente de segurança ([vulnerabilidades](vulnerabilidades.md)) além de rotacionar.

## Não testado

Nenhuma troca foi feita em produção. A seção "Trocar um segredo" do `docs/deploy.md` foi escrita a partir dos arquivos de infra, não executada; o comportamento de `umami_api_key` depois de trocar o `umami_app_secret` também não foi observado.
