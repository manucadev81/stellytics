# Runbook: failover MANUAL do banco (réplica do Postgres)

Vale só para quem ligou a replicação (`ha_replication: true` em `site.yml` e `standby.yml`, ver "Alta disponibilidade" em [`docs/deploy.md`](../deploy.md) e o [ADR 0009](../adr/0009-alta-disponibilidade.md), degrau 2). Sem réplica, o caminho é o [backup e restauração](backup-e-restauracao.md).

> **Não há failover automático.** Com dois nós e sem testemunha, promover sozinho arriscaria dois primários gravando ao mesmo tempo (split-brain). Quem decide e executa é uma pessoa. **Nenhum passo deste runbook foi ensaiado numa produção real** (rede privada e VMs da Hetzner, Caddy com certificado, DNS). O que foi exercitado, em Docker local, está em "O que foi testado" no fim.

## RPO e RTO esperados (estimativas, não medições de produção)

- **RPO (dados que podem se perder):** a replicação é **assíncrona**. A perda é o WAL que o primário gerou e a réplica ainda não recebeu quando ele caiu. Em Docker local, sem carga, a linha nova apareceu na réplica em cerca de 60 ms (incluindo o `docker exec`). Entre `nbg1` e `fsn1`, com carga real, espere de frações de segundo a poucos segundos, **sem medição**. Se a VM e o volume do primário forem perdidos juntos, a réplica ainda é o melhor ponto de partida; o backup do WAL-G (RPO de cerca de 1 min, `archive_timeout=60`) fica de segunda linha.
- **RTO (indisponibilidade):** soma de detecção (o alerta `EndpointDown` leva pelo menos 1 min 45 s, ADR 0009), tempo até uma pessoa agir (hoje a operação é de uma só, sem plantão) e a execução abaixo. A promoção em si levou **165 ms** no teste local e trocar o contêiner por um `postgres` do `prod.yaml` levou 12 s; o `site.yml` na VM da réplica (pull de imagens, `cosign verify`, build, Caddy pedindo certificado) **não foi medido** e deve levar dezenas de minutos. Estimativa de ponta a ponta: **30 a 90 minutos**, dominada pela presença humana. Isso **não** sustenta 99,99% e, por si só, consome o orçamento mensal de 99,9% (43 min).

## Quando usar

- A VM `stellytics` (ou o volume do banco, ou o `nbg1`) está fora e não volta em minutos, e a réplica está saudável.
- **Não use** para um erro lógico (migração ruim, dado apagado): a réplica reproduz o erro. Para isso, restauração a um horário (`backup-e-restauracao.md`).

## Limite do desenho atual

No `prod.yaml`, o portal e o Umami falam com `postgres:5432` (o serviço do mesmo Compose). **Não dá para repontar só o banco** e manter a pilha na VM antiga. O failover suportado é **mover a pilha inteira para a VM da réplica** (passo 4). A VM da réplica foi criada com o firewall só de SSH e ping; é preciso abrir 80 e 443 nela.

## Antes de promover

1. **Confirme que o primário não vai voltar a gravar** (nos comandos abaixo, na VM da réplica, `cd /opt/stellytics/infra/compose` primeiro). Desligue a VM antiga pelo console ou `hcloud server poweroff stellytics`. Isso é o que impede o split-brain; faça mesmo que ela "pareça" morta. Se não conseguir desligar, não promova sem decidir conscientemente o risco.
2. **Veja o quanto a réplica recebeu** (na VM da réplica, ou pelo túnel `infra/scripts/admin-tunnel.sh <ip-da-réplica>`):

   ```bash
   cd /opt/stellytics/infra/compose
   docker compose -f prod-standby.yaml exec -T postgres psql -U postgres -d stellytics -Atc \
     "SELECT pg_is_in_recovery(), pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn(), now() - pg_last_xact_replay_timestamp()"
   ```

   O último valor é o atraso aparente (só serve se o primário ainda gravava). Anote a hora e o LSN: é o que vai constar como RPO no relato.

## Passo a passo

### 1. Promover a réplica

```bash
docker compose -f prod-standby.yaml exec -T postgres psql -U postgres -d postgres -Atc \
  "SELECT pg_promote(wait => true, wait_seconds => 60)"   # t
docker compose -f prod-standby.yaml exec -T postgres psql -U postgres -d stellytics -Atc "SELECT pg_is_in_recovery()"   # f
```

A promoção é irreversível: o nó passa a uma nova linha do tempo e não volta a ser réplica sem ser refeito (passo 6).

### 2. Parar o contêiner da réplica

```bash
docker compose -f prod-standby.yaml down      # sem -v: o diretório de dados é um bind mount e fica
```

### 3. Trocar a VM no inventário e nas variáveis

Em `infra/ansible/inventory/production.yml` (fora do git), faça o host `stellytics` apontar para a VM da réplica e para o **volume dela**:

```yaml
    stellytics:
      ansible_host: <IP público da VM stellytics-db-standby>
      data_volume_device: /dev/disk/by-id/scsi-0HC_Volume_<id do volume stellytics-data-standby>
```

Em `secrets.yml` ou `-e`, ponha `ha_private_ip: 10.50.0.30` (o IP privado da réplica; vale em `site.yml` com `ha_replication: true`, para o Postgres e o Umami publicarem nele). Mantenha `walg_libsodium_key`, a chave de API do Umami e os demais segredos **iguais**.

### 4. Subir a pilha completa na VM da réplica

Abra 80 e 443 nela (hoje o firewall é só SSH e ping; pela console da Hetzner ou `hcloud firewall apply-to-resource stellytics --type server --server stellytics-db-standby`, comando não ensaiado) e rode:

```bash
cd infra/ansible
ansible-playbook site.yml -e @secrets.yml -e ha_replication=true -e ha_private_ip=10.50.0.30
```

O `prod.yaml` sobe o `postgres` **no mesmo diretório de dados** (`/srv/stellytics/postgres`), já promovido: não há `initdb` nem cópia. O papel `backup` instala o timer do `stellytics-backup` na nova VM, e o WAL passa a ir ao mesmo prefixo do Object Storage em uma nova linha do tempo. Faça um backup base logo (`systemctl start stellytics-backup`): o último é da linha do tempo antiga.

### 5. DNS e ingestão

- Aponte `portal_host` e `ingest_host` (A e AAAA) para a VM nova. O TTL define o quanto isso demora; o IP flutuante, quando ligado, **não** tem configuração na VM pelo Ansible (pendente), então não conte com ele.
- As VMs de ingestão extras leem o banco e o Umami do primário. Repontar: `ingest_db_host: 10.50.0.30` e `ingest_umami_host: 10.50.0.30` (em `-e`), e `ansible-playbook ingest.yml -e @secrets.yml ...`. Enquanto isso, esses gateways devolvem 503 quando o cache de `ingest_site` (1 min) vence.
- Rode a conferência de `docs/deploy.md` (passo 6) e o monitoramento, que sonda os hosts pelo nome.

### 6. O que fazer com o antigo primário

- **Não ligue a pilha antiga.** O `postgres` tem `restart: unless-stopped` e o Docker sobe sozinho com a VM: se ela for religada, o primário antigo volta a gravar (split-brain). Antes de religá-la, **remova a VM ou desanexe e apague o volume antigo** (ou, no mínimo, `systemctl disable docker` e `docker compose -f prod.yaml down` por console). Dois primários gravando = dados divergentes sem como mesclar. Como rede de proteção, a réplica recusa subir sobre um diretório que seja de um primário (sem `standby.signal`, erro explícito; `STANDBY_FORCE=1` desliga a proteção, não use), mas isso só protege a montagem da réplica, não o `prod.yaml` da VM antiga.
- O que ele gravou e não chegou à réplica **está perdido** para a produção (é o RPO). Se importar, antes de apagar, suba uma cópia **isolada** (sem rede) do diretório de dados dele só para inspecionar; não há mesclagem automática.
- Para reaproveitá-lo como réplica da nova: apague o diretório de dados dele (`/srv/stellytics/postgres`, depois de ter certeza de que não precisa) e rode `standby.yml` com o host `db_standby` apontando para ele e `ha_primary_private_ip` para o IP do novo primário. O `pg_basebackup` refaz tudo; o slot é criado pelo próprio script. Isto foi exercitado em Docker (passo D do teste).
- Se for recriar tudo do zero, reconcilie o estado do OpenTofu com o que foi mudado à mão (volume, firewall, qual VM é a principal) antes do próximo `tofu apply`; senão ele pode tentar desfazer o failover.

## Disco e monitoramento

- **Folga de disco:** mantenha no volume do primário pelo menos **10 GiB livres além do crescimento esperado do banco, backups locais e outros WAL**, com margem adicional para a carga entre checkpoints. O `max_slot_wal_keep_size` padrão é 10 GB; ele limita a retenção do slot em checkpoints, não é teto absoluto de uso do disco. Ao perder WAL necessário, o slot pode ser invalidado e a réplica precisa ser refeita.
- **Alertas, quando a sonda estiver ligada:** `ReplicationStopped` dispara se o slot não existe ou deixa de transmitir por 2 minutos; `ReplicationLagHigh`, se a diferença entre o WAL atual e o replay ultrapassa 256 MiB por 5 minutos; `ReplicationSlotWALHigh`, se o slot retém mais de 5 GiB por 2 minutos. `ReplicationMonitorDown` avisa se a sonda ou a consulta ao primário falha por 2 minutos. O atraso é medido em bytes, não em tempo: confirme o RPO com os LSNs antes de promover. Consulte também `df -h /srv/stellytics`; os alertas não substituem um alerta de disco livre.
- **Volume:** o papel `data_volume` **não formata** nada: só confere o dispositivo, grava o `fstab` e monta (o ext4 vem do OpenTofu na criação do volume). Um volume sem sistema de arquivos falha ao montar; um volume com dados é montado como está, sem apagar.

## Depois

1. Registre o incidente por escrito: hora da queda, hora da promoção, LSN e RPO real.
2. Confira o backup: `wal-g backup-list`, `pg_stat_archiver` (`archived_count` subindo, `last_failed_wal` vazio).
3. Monte uma nova réplica (passo 6) para voltar ao degrau 2. Até lá, a produção está de novo com um banco só.
4. Corrija este runbook com o que a execução real ensinou.

## O que foi testado (Docker local) e o que não foi

Testado por `infra/compose/test/ha-replication-test.sh`: replicação em streaming (50 mil linhas com mesmo checksum), réplica somente leitura, queda do primário, `pg_promote`, a réplica promovida rodando como o `postgres` do `prod.yaml` no mesmo diretório de dados, arquivamento de WAL e backup base com WAL-G depois da promoção, gateways lendo `ingest_site` do banco promovido, e o antigo primário refeito como réplica.

**Não testado:** a rede privada, os firewalls e o IP flutuante da Hetzner; balanceador; failover automático; carga e atraso de replicação sob carga; o `site.yml` completo na VM da réplica; restauração do WAL-G atravessando a troca de linha do tempo; repontar DNS; o `hcloud firewall apply-to-resource`.
