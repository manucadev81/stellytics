# Runbook: backup e restauração

A descrição completa (WAL contínuo, backup diário às 03:15 UTC, retenção de 7, passo a passo da restauração e voltar a um horário) está em [`docs/deploy.md`](../deploy.md), seções "Backup" e "Restoring the database", e a decisão no [ADR 0004](../adr/0004-hospedagem-e-backup.md). **Não repita os blocos de comandos daqui: use os de lá.** Este runbook diz quando acionar, como diagnosticar e como conferir.

Os scripts ficam em [`infra/compose/postgres/`](../../infra/compose/postgres/): `backup.sh` vira `stellytics-backup` e `restore.sh` vira `stellytics-restore` dentro da imagem do Postgres (`Dockerfile`).

## Sintomas

- Não há backup recente em `backup-list`, ou o `stellytics-backup.service` falhou.
- O arquivamento de WAL parou (o `archive_command` falha e o disco do Postgres enche com WAL pendente).
- Perda ou corrupção de dados, ou da VM e do volume (restauração necessária).
- Antes de uma atualização com migração nova (backup preventivo).

## Diagnóstico

Na VM:

```bash
cd /opt/stellytics/infra/compose
systemctl status stellytics-backup.service stellytics-backup.timer
systemctl list-timers stellytics-backup.timer
journalctl -u stellytics-backup.service -n 100 --no-pager
docker compose -f prod.yaml exec -u postgres postgres wal-g backup-list
docker compose -f prod.yaml exec -u postgres postgres wal-g backup-list --detail
docker compose -f prod.yaml logs --tail 200 postgres | grep -i -E 'archive|wal-g'
docker compose -f prod.yaml exec -u postgres postgres psql -c 'select * from pg_stat_archiver'
df -h /srv/stellytics
```

Causas comuns: credencial do Object Storage inválida ou expirada (`walg_s3_access_key` e `walg_s3_secret_key`), endpoint ou prefixo errados (`walg_s3_endpoint`, `walg_s3_prefix`), bucket removido, rede, `walg_libsodium_key` diferente da usada nos backups antigos (ver "Chave de cifra" abaixo), disco cheio.

## Ação

- **Backup base agora:** `systemctl start stellytics-backup` (executa `stellytics-backup`: `wal-g backup-push`, depois `wal-g delete retain FULL 7 --confirm` e `backup-list`).
- **Credencial trocada:** siga [rotação de segredos](rotacao-de-segredos.md) e confira com `backup-list`.
- **Restaurar:** siga "Restoring the database" em `docs/deploy.md`. Atenção: ela derruba portal, ingestão (eventos descartados) e Umami até terminar; exige `systemctl stop stellytics-backup.timer` antes (e `start` depois) e espaço livre para dois conjuntos de dados (`df -h /srv/stellytics`), porque os dados antigos ficam em `postgres.antes-*`. Siga também a janela de manutenção, o `mv` dos dados antigos para `postgres.antes-*` e o "voltar a um horário" (`stellytics-restore <backup> '<horário>'`). Para escolher entre restaurar o último ou um horário: um erro lógico (migração ruim, remoção indevida) pede o horário anterior ao erro, com um backup base concluído antes dele; perda de disco pede o `LATEST`.
- **Perda de VM e de volume ao mesmo tempo:** recrie a infraestrutura (`docs/deploy.md`, passos 1 a 4) com o **mesmo** `secrets.yml` (em especial `walg_libsodium_key` e as credenciais do bucket) e restaure em seguida. O bucket de backup tem `prevent_destroy`; os backups sobrevivem.

## Chave de cifra

Os backups são cifrados no cliente com `walg_libsodium_key` (libsodium, 64 hexadecimais). **Sem essa chave não há como ler nenhum backup, e ela não pode ser trocada por rotina**: o `docs/deploy.md` é explícito, e o `prod.yaml` tem uma chave só, então um restore de backup antigo exige a chave antiga. Guarde uma cópia do `secrets.yml` no cofre de senhas, fora da VM e do repositório.

Se a chave **vazar**, a resposta (ainda **não testada**) é: criar um bucket ou prefixo novo (`walg_s3_prefix`, `backup_bucket` no OpenTofu), gerar uma chave nova, aplicar e rodar `systemctl start stellytics-backup` para um backup base novo, e **manter o prefixo antigo, com a chave antiga guardada**, enquanto houver retenção útil nele. Quem tiver a chave vazada e acesso ao bucket antigo lê esses backups: restrinja as credenciais do bucket antigo ([rotação](rotacao-de-segredos.md)).

## Verificação

- `wal-g backup-list` mostra um backup recente (data de hoje depois de `stellytics-backup`).
- `pg_stat_archiver` com `last_archived_time` de poucos minutos e `failed_count` sem crescer.
- Depois de uma restauração: `docker compose -f prod.yaml ps` todo `healthy`, os checks do passo 6 de `docs/deploy.md`, entrar no portal e abrir o painel de um site, e um novo `systemctl start stellytics-backup`. Só apague `postgres.antes-*` depois disso.
- **Ensaio:** a restauração é testada em todo PR pelo job `infra` do CI (`infra/compose/test/prod-stack-test.sh`, com S3 local). Contra o bucket real, ensaie numa VM descartável antes de precisar (pendência registrada em `docs/deploy.md`); marque aqui a data do primeiro ensaio: **nunca realizado**.

## Quando escalar

- `backup-list` vazio ou sem backup novo por mais de 2 dias e a causa não é credencial ou rede.
- O Postgres não abre depois da restauração (log do contêiner `postgres`).
- Suporte da Hetzner para o Object Storage fora do ar ou volume corrompido.
- Perda da `walg_libsodium_key` (ou vazamento dela: ver "Chave de cifra"): os backups não são recuperáveis; trate como perda total de histórico.

## Não testado

Restauração contra o bucket real da Hetzner, o `journalctl` e as units em VM real, o `pg_stat_archiver` no ambiente de produção e a troca da chave de cifra.
