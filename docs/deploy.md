# Deploy da produção

Como recriar a produção do notify do zero, só com este repositório e os segredos ([#14](https://github.com/romulospl/notify/issues/14)). As decisões estão no [ADR 0004](adr/0004-hospedagem-e-backup.md); os arquivos, em [`infra/`](../infra/README.md).

> **Escopo.** Este guia sobe o que está em [`infra/compose/prod.yaml`](../infra/compose/prod.yaml): Caddy, portal, gateway de ingestão web, Umami e Postgres com backup, mais o monitoramento externo. **O coletor de API e o job de consolidação não entram** (ver [O que o guia ainda não cobre](#o-que-o-guia-ainda-não-cobre)): em produção, hoje, só as métricas web funcionam. Ninguém de fora seguiu este guia de ponta a ponta ainda; o que foi conferido sem credenciais está em [Como este guia foi conferido](#como-este-guia-foi-conferido).

## Visão geral

```mermaid
flowchart LR
  internet((Internet)) -->|80, 443| caddy
  subgraph vm["VM na Hetzner (nbg1, Alemanha)"]
    caddy[Caddy] --> portal[Portal]
    caddy -->|/e /c /s/ /healthz| gw[Gateway ingest-web]
    portal --> umami[Umami]
    gw --> umami
    portal --> pg[(Postgres + WAL-G)]
    gw --> pg
    umami --> pg
  end
  pg -->|WAL contínuo e backup diário, cifrados| s3[(Object Storage nbg1)]
```

- Só o Caddy tem porta pública. O firewall da Hetzner abre 80 e 443 para todos e 22 só para as redes de administração.
- Os dados do Postgres e os certificados do Caddy ficam num volume da Hetzner montado em `/srv/notify`, que sobrevive se a VM for recriada. Sem o volume montado, o Docker e o backup não sobem (para não criar um banco vazio no disco da VM).
- Os logs do Caddy não guardam IP, cabeçalhos nem URL dos pedidos.

## Onde ficam os dados (data residency)

| O quê | Onde |
|---|---|
| VM e volume com o Postgres (bancos `notify` e `umami`) | Hetzner, Nuremberg (`nbg1`), Alemanha |
| Backups do Postgres (WAL e backups base), cifrados no cliente | Hetzner Object Storage, Nuremberg (`nbg1`), Alemanha |

O local é a variável `location` do OpenTofu. Se mudar, mude também `walg_s3_endpoint` e `walg_s3_region` no Ansible.

## O que você precisa

**Contas e serviços:**

| O quê | Para quê |
|---|---|
| Projeto na Hetzner Cloud, com o Object Storage habilitado | VMs, volume, firewall e o bucket do backup |
| Um domínio com controle do DNS (qualquer provedor) | Dois nomes: portal e ingestão (passo 2) |
| Um provedor de SMTP (envio autenticado) | Confirmação de e-mail no cadastro e recuperação de senha do portal. **Sem ele ninguém consegue se cadastrar**, porque o e-mail confirmado é obrigatório. Formato da URL: `smtps://usuario:senha@host:465` |
| Um e-mail para o Let's Encrypt (`acme_email`) | Avisos de certificado |
| Uma conta no GitHub com permissão para publicar releases do repositório (ou o seu fork) | Gerar as imagens assinadas (passo 3) |
| Opcional: um serviço de sinal de vida (healthchecks.io ou similar) | Avisa se a própria VM de monitoramento cair |

**Ferramentas na sua máquina:**

- `git`, `ssh` e `openssl`;
- OpenTofu 1.13.2 ou mais novo (ou o contêiner `ghcr.io/opentofu/opentofu:1.13.2`) e Ansible (ansible-core 2.21; o deploy usa só `ansible.builtin`, sem coleções extras);
- Docker com buildx e `jq`, para ler os digests das imagens (ou pegue os digests na página do pacote no GitHub);
- Node 24 ou mais novo e pnpm 10.34.5 (`corepack enable`), para os comandos de administração do passo 5.

Clone o repositório e instale as dependências. Todos os comandos abaixo partem da raiz do clone:

```bash
git clone https://github.com/romulospl/notify.git && cd notify
corepack enable && pnpm install
```

**Segredos** (nunca no repositório):

| Segredo | Onde é usado | Como obter |
|---|---|---|
| `HCLOUD_TOKEN` | OpenTofu | Console da Hetzner → projeto → Security → API tokens (leitura e escrita) |
| `TF_VAR_object_storage_access_key` e `TF_VAR_object_storage_secret_key` | OpenTofu (cria o bucket) e Ansible (`walg_s3_access_key` e `walg_s3_secret_key`) | Console da Hetzner → projeto → Object Storage → credenciais S3 |
| `secrets.yml` | Ansible | Copie `infra/ansible/secrets.example.yml` e preencha (veja abaixo) |

No `secrets.yml`, todos estes campos são **obrigatórios** (o playbook para se algum estiver vazio): `postgres_password`, `umami_db_password`, `notify_db_password`, `notify_ingest_db_password`, `notify_collector_db_password`, `umami_app_secret`, `portal_auth_secret`, `ingest_proof_secret`, `smtp_url`, `walg_s3_access_key`, `walg_s3_secret_key` e `walg_libsodium_key`. Gere cada senha ou chave com `openssl rand -hex 32` (64 caracteres; o `ingest_proof_secret` exige pelo menos 32, e a `walg_libsodium_key` exatamente 64 hexadecimais). O `umami_api_key` começa vazio e só existe depois do passo 5. O `secrets.yml` e o `inventory/production.yml` estão no `.gitignore`.

A chave `walg_libsodium_key` cifra os backups: **sem ela, nenhum backup pode ser restaurado**. Guarde uma cópia no cofre de senhas do time, fora da VM.

**Se você faz um fork** (em vez de usar as imagens do `romulospl/notify`): troque `ghcr.io/romulospl/notify/...` pelas suas imagens e o `cosign_identity_regexp` pelo caminho do `release.yml` do seu fork, em `group_vars/all.yml`. O playbook recusa imagens cuja assinatura não bata com essa identidade.

## 1. Provisionar a infraestrutura (OpenTofu)

1. Copie `infra/opentofu/production.tfvars` para `infra/opentofu/production.secret.tfvars` (esse nome está no `.gitignore`) e troque todos os valores `CONFIRA`: `server_type` e `monitor_server_type` (confira com `hcloud server-type list`; escolha tipos com a mesma arquitetura, x86 ou Arm, das imagens publicadas, que são `linux/amd64`), `backup_bucket` (nome único em toda a Hetzner), `admin_ssh_public_key` e `admin_cidrs` (os IPs de onde você administra, nunca `0.0.0.0/0`). Se o seu IP mudar, atualize `admin_cidrs` e aplique de novo, ou o SSH fica fechado.
2. Rode (com o `-var-file` que você criou):

   ```bash
   export HCLOUD_TOKEN=...
   export TF_VAR_object_storage_access_key=...
   export TF_VAR_object_storage_secret_key=...
   tofu -chdir=infra/opentofu init
   tofu -chdir=infra/opentofu apply -var-file=production.secret.tfvars
   tofu -chdir=infra/opentofu output
   ```

   As saídas trazem os IPs da VM (`server_ipv4`, `server_ipv6`) e da VM de monitoramento (`monitor_ipv4`), o dispositivo do volume (`data_volume_device`) e o endpoint e o prefixo do backup (`backup_s3_endpoint`, `backup_s3_prefix`). Guarde-as: os passos 2 e 4 usam todas.
3. Espere um ou dois minutos: o cloud-init atualiza a VM na primeira subida. Confira que o SSH entra: `ssh root@<server_ipv4> true` (aceite a chave do host; o Ansible recusa hosts desconhecidos, `host_key_checking = True`).

A VM, o volume e o bucket têm proteção contra remoção (`delete_protection`, `prevent_destroy`): o `tofu destroy` falha de propósito.

## 2. DNS

Crie dois registros A (e AAAA, com o IPv6) apontando para a VM:

- o host do portal, por exemplo `app.<domínio>`;
- o host da ingestão, por exemplo `e.<domínio>`. Ele não pode ter `umami`, `analytics`, `track` nem `collect`, que os bloqueadores de anúncio barram.

Use os IPs `server_ipv4` (A) e `server_ipv6` (AAAA). O Caddy só consegue o certificado depois que os dois nomes resolvem para a VM; confira com `dig +short app.<domínio>` antes do passo 4. Se o DNS ainda não propagou quando a pilha sobe, o Caddy tenta de novo sozinho; não precisa refazer o deploy.

## 3. Publicar as imagens

O deploy só aceita imagens publicadas pelo workflow de release, assinadas com cosign e referenciadas por digest.

1. Publique uma release no GitHub (`vX.Y.Z`; o repositório ainda não tem nenhuma). O workflow `release.yml` gera o SBOM e publica no GHCR uma imagem para cada `Dockerfile` de `apps/*` e `services/*`, assinada com cosign. Para o deploy interessam `ghcr.io/romulospl/notify/portal` e `ghcr.io/romulospl/notify/ingest-web`, com a tag **sem o `v`** (`X.Y.Z`); `collector` e `rollup` também são publicadas, mas não entram no deploy (ver o fim do guia). Acompanhe o workflow em Actions até todos os jobs ficarem verdes.
2. **Deixe os pacotes públicos.** Pacotes novos do GHCR nascem privados, e a VM baixa as imagens e confere a assinatura sem `docker login`. Em GitHub → Packages → cada pacote (`notify/portal`, `notify/ingest-web`) → Package settings → Change visibility → Public. Sem isso, o passo 4 falha em `cosign verify` ou no `docker compose up` com `unauthorized`.
3. Pegue os digests:

   ```bash
   docker buildx imagetools inspect ghcr.io/romulospl/notify/portal:X.Y.Z --format '{{json .Manifest}}' | jq -r .digest
   docker buildx imagetools inspect ghcr.io/romulospl/notify/ingest-web:X.Y.Z --format '{{json .Manifest}}' | jq -r .digest
   ```

## 4. Deploy (Ansible)

1. Em `infra/ansible/`:
   - copie `inventory/production.example.yml` para `inventory/production.yml` e preencha com as saídas do OpenTofu (`server_ipv4` e `data_volume_device` no host `notify`; `monitor_ipv4` no host `monitor`, usado só no passo de monitoramento);
   - copie `secrets.example.yml` para `secrets.yml` e preencha.
2. Em `group_vars/all.yml`, preencha:
   - os hosts (`portal_host`, `ingest_host`), `acme_email` e `mail_from` (remetente dos e-mails do portal; o domínio dele precisa estar autorizado no seu SMTP);
   - `portal_image` e `ingest_web_image`, no formato `ghcr.io/romulospl/notify/portal@sha256:<digest>` (o playbook recusa imagem sem digest);
   - `walg_s3_prefix`, com a saída `backup_s3_prefix`. O `walg_s3_endpoint` e o `walg_s3_region` já vêm certos para `nbg1`.
3. Confira a conexão e rode, de `infra/ansible/`:

   ```bash
   ansible notify -m ping
   ```

   ```bash
   ansible-playbook site.yml -e @secrets.yml
   ```

Na primeira vez leva alguns minutos (atualizações, Docker, build da imagem do Postgres com WAL-G). O playbook prepara a VM e sobe a aplicação:

- atualiza a VM e liga as atualizações automáticas de segurança;
- instala o Docker (com a chave do repositório conferida) e o cosign;
- monta o volume;
- copia o Compose, o Caddyfile e o `.env` (permissão 600) para `/opt/notify`;
- confere a assinatura das imagens;
- sobe a pilha (as migrações do portal rodam nessa subida) e liga o timer do backup diário.

Nessa primeira subida, o `UMAMI_API_KEY` está vazio: o portal funciona (login, cadastro), mas ainda não cria sites no Umami. O passo 5 resolve.

Ele pode rodar de novo quantas vezes for preciso; sem mudança, não altera nada.

## 5. Primeira subida

1. **Túnel de administração** (deixe aberto num terminal):

   ```bash
   infra/scripts/admin-tunnel.sh <ip-da-vm>
   ```

   Postgres em `127.0.0.1:15432`, Umami em `http://127.0.0.1:13001`. Nada disso fica exposto na internet.
2. **Troque a senha do administrador do Umami.** Ele nasce com `admin`/`umami` (alcançável só pelo túnel). Entre em `http://127.0.0.1:13001` pelo túnel e troque, antes de qualquer outro passo.
3. **Conta de serviço do Umami** (o portal usa a chave dela), da raiz do clone e com o túnel aberto:

   ```bash
   read -rs UMAMI_ADMIN_PASSWORD && export UMAMI_ADMIN_PASSWORD   # a senha nova do passo 2
   UMAMI_URL=http://127.0.0.1:13001 pnpm --filter @notify/portal umami:service-account
   ```

   A chave impressa na saída padrão vai em `umami_api_key` no `secrets.yml`. Rode o playbook de novo (`ansible-playbook site.yml -e @secrets.yml`): ele recria só o portal com a chave.
4. **Primeiro operador do SCF** (conta com visão global; nasce com e-mail já confirmado), também com o túnel aberto:

   ```bash
   read -rs OPERATOR_PASSWORD && export OPERATOR_PASSWORD   # mínimo de 12 caracteres
   DATABASE_URL='postgresql://notify:<notify_db_password>@127.0.0.1:15432/notify' \
     pnpm --filter @notify/portal operator:create --email <e-mail> --name "<nome>"
   ```

   Os outros operadores saem de `operator:promote` (conta existente que não seja de projeto) com o mesmo `DATABASE_URL`.
5. **Primeiro backup base**, sem esperar o horário do timer:

   ```bash
   ssh root@<ip-da-vm> systemctl start notify-backup
   ```

   Confira com `backup-list` (seção Backup): deve aparecer um backup.

## 6. Conferir a produção

Sem login, de qualquer máquina:

```bash
curl -fsS https://<portal_host>/api/auth/ok            # portal: {"ok":true}
curl -fsS https://<ingest_host>/healthz                # gateway: ok
curl -fsS "https://<ingest_host>/c?s=00000000-0000-4000-8000-000000000000"  # desafio {"t":...,"d":12}
curl -s -o /dev/null -w '%{http_code}\n' https://<ingest_host>/api/send    # 404: o Umami não tem rota pública
curl -s -o /dev/null -w '%{http_code}\n' https://<portal_host>/api/send    # 404 também (é o portal, não o Umami)
```

Na VM (`ssh root@<ip-da-vm>`):

```bash
cd /opt/notify/infra/compose
docker compose -f prod.yaml ps            # todos healthy
ss -tlnp | grep -E ':(80|443|5432) '      # 80/443 em todas as interfaces; 5432 só em 127.0.0.1
systemctl list-timers notify-backup.timer
```

No navegador, o teste que prova o conjunto:

1. Em `https://<portal_host>/sign-up`, crie uma conta com um e-mail seu, confirme pelo link recebido (se o e-mail não chega, o problema é o `smtp_url`/`mail_from`: `docker compose -f prod.yaml logs portal`), crie o projeto em `/onboarding` e cadastre um site em `/sites`.
2. Instale no site o script (o gateway serve `https://<ingest_host>/s/v<versão>/n.js`; versões e hashes SRI em [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json); trechos de instalação em [`examples/README.md`](../examples/README.md)), abra uma página e, em alguns minutos, veja a visita no painel do site.
3. Entre com a conta de operador em `/operator` e confira que o projeto aparece.

Se algo falhar, os logs de cada serviço estão em `docker compose -f prod.yaml logs <serviço>` (portal, ingest-web, umami, caddy, postgres). Os logs não guardam IP nem cabeçalhos dos visitantes.

## Atualizar a aplicação

1. Publique uma release nova (passo 3) e deixe os pacotes novos públicos, se for a primeira vez de algum.
2. Troque os digests em `group_vars/all.yml` e rode `ansible-playbook site.yml -e @secrets.yml`. O playbook confere a assinatura e recria só os contêineres cuja imagem mudou. As migrações do portal rodam na subida (`RUN_MIGRATIONS=true`).
3. Repita a conferência do passo 6.

**Antes de uma versão com migração nova**, force um backup base (`systemctl start notify-backup`): as migrações só andam para frente.

**Voltar atrás (rollback).** Ponha em `group_vars/all.yml` os digests da versão anterior e rode o playbook de novo. Isso volta o código, **não o banco**: se a versão nova trouxe uma migração, o esquema continua o novo, e a versão antiga pode não funcionar com ele. Nesse caso, restaure o banco do backup anterior à atualização (seção "Restaurar o banco") antes de subir a imagem antiga. Versões do script de rastreamento já publicadas continuam disponíveis em `/s/` (são imutáveis), então páginas dos tenants não quebram.

**Trocar um segredo.** Mudar uma senha em `secrets.yml` só atualiza o `.env`; os usuários do banco são criados uma vez, no volume vazio. Para `notify_ingest_db_password` e `notify_collector_db_password`, reaplique `02-ingest-roles.sh` (ver `CLAUDE.md`, "Ambiente local"; na VM: `docker compose -f prod.yaml exec -T postgres sh < /opt/notify/infra/compose/postgres/init/02-ingest-roles.sh`). Para `postgres`, `umami` e `notify`, rode `ALTER ROLE ... PASSWORD` por psql antes de aplicar o playbook. A `walg_libsodium_key` não pode ser trocada: os backups antigos deixariam de abrir.

## Backup

- **WAL contínuo:** o Postgres manda cada segmento de WAL ao Object Storage pelo `archive_command`, no máximo 60 segundos depois da escrita (`archive_timeout`).
- **Backup base diário** às 03:15 UTC, pelo timer `notify-backup.timer`. Mantém os 7 mais novos (`walg_retain_full`).
- **Ver os backups:**

  ```bash
  cd /opt/notify/infra/compose
  docker compose -f prod.yaml exec -u postgres postgres wal-g backup-list
  ```

- **Rodar um agora:** `systemctl start notify-backup`.
- A restauração é testada a cada PR pelo job `infra` do CI (`infra/compose/test/prod-stack-test.sh`): ela restaura num contêiner novo e confere que voltou também o que foi gravado depois do backup base.

## Restaurar o banco

Na VM, como root:

```bash
cd /opt/notify/infra/compose
systemctl stop notify-backup.timer
docker compose -f prod.yaml stop caddy portal ingest-web umami postgres

# Guarda os dados atuais (não apague até conferir a restauração).
mv /srv/notify/postgres /srv/notify/postgres.antes-$(date +%F-%H%M)
mkdir /srv/notify/postgres

# Traz o backup mais novo e todo o WAL depois dele.
docker compose -f prod.yaml run --rm --no-deps --entrypoint sh postgres -c \
  'chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA" && su postgres -c "notify-restore LATEST"'

docker compose -f prod.yaml up -d --wait
systemctl start notify-backup.timer
systemctl start notify-backup   # novo backup base logo depois da restauração
```

Na subida, o Postgres aplica o WAL arquivado e abre o banco numa linha do tempo nova; o WAL-G continua arquivando a partir dela.

**Voltar a um horário** (por exemplo, antes de um erro às 02:00): um backup base já contém tudo o que aconteceu até ele terminar, então o ponto de partida precisa ser um backup **concluído antes** do horário escolhido. Liste os backups e use o nome do último que terminou antes dele:

```bash
docker compose -f prod.yaml run --rm --no-deps postgres wal-g backup-list --detail

# No lugar do notify-restore LATEST do bloco acima:
docker compose -f prod.yaml run --rm --no-deps \
  -e BACKUP=<nome-do-backup> -e TARGET='2026-11-14 01:59:00+00' --entrypoint sh postgres -c \
  'chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA" && su postgres -c "notify-restore \"$BACKUP\" \"$TARGET\""'
```

O `notify-restore` descarta os alvos de recuperação que vieram dentro do backup (de uma restauração anterior), então cada restauração usa só o alvo pedido.

## Estado do OpenTofu

O estado fica em `infra/opentofu/terraform.tfstate`, na máquina de quem aplica (fora do git, porque tem dados da infraestrutura). Para compartilhar entre pessoas, guarde-o no próprio Object Storage: acrescente o bloco `backend` **dentro** do bloco `terraform` que já existe em `versions.tf`:

```hcl
terraform {
  # ... required_version e required_providers como estão ...

  backend "s3" {
    bucket                      = "<bucket-do-estado>"
    key                         = "notify/production.tfstate"
    region                      = "nbg1"
    endpoints                   = { s3 = "https://nbg1.your-objectstorage.com" }
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
    use_path_style              = true
  }
}
```

Depois rode `tofu init -migrate-state`. As credenciais vêm de `AWS_ACCESS_KEY_ID` e `AWS_SECRET_ACCESS_KEY`.

## Monitoramento

Uma VM separada, em outro data center (`fsn1`), testa de fora o portal e a ingestão a cada 15 segundos e manda e-mail quando algo cai ([ADR 0005](adr/0005-monitoramento-externo.md)). Ela é criada pelo mesmo `tofu apply` (saída `monitor_ipv4`).

Faça depois do deploy (passo 4) e do DNS, senão o monitor já nasce alertando.

1. No `inventory/production.yml`, confirme o host `monitor` com o `monitor_ipv4` (veja o `.example`). Aceite a chave SSH dele uma vez: `ssh root@<monitor_ipv4> true`.
2. Em `group_vars/all.yml`, preencha `monitor_alert_email`, `monitor_smtp_smarthost` (`host:porta`), `monitor_smtp_from` e, se o SMTP pedir login, `monitor_smtp_username`. No `secrets.yml`, preencha `monitor_smtp_password`.
3. **Recomendado:** configure um serviço externo de sinal de vida (qualquer um que receba um POST periódico e avise quando ele parar) e ponha a URL em `monitor_deadman_url`. Sem isso, se a VM de monitoramento cair, ninguém fica sabendo.
4. Rode, de `infra/ansible/`:

   ```bash
   ansible-playbook monitoring.yml -e @secrets.yml
   ```

| Alerta | Quando |
|---|---|
| `EndpointDown` | Portal, gateway web ou ingestão de API sem resposta 2xx por 1 minuto |
| `TLSCertificateExpiringSoon` | Certificado vencendo em menos de 14 dias (a renovação automática falhou) |
| `ProbeFailing` | O Prometheus não consegue nem pedir o teste de um alvo ao blackbox |
| `ProberDown` | O blackbox exporter, que faz os testes, está fora do ar |

Confira que o monitoramento enxerga a produção: no Prometheus (túnel abaixo), a página *Status → Targets* (ou a consulta `probe_success`) deve mostrar `1` para o portal e o gateway. Para testar o e-mail de alerta de verdade, na VM de produção rode `docker compose -f prod.yaml stop ingest-web`, espere uns 2 minutos pelo `EndpointDown` e suba de novo (`start ingest-web`). O Alertmanager e o Prometheus ficam só no loopback; para vê-los, abra um túnel e acesse `http://127.0.0.1:9090` e `http://127.0.0.1:9093`:

```bash
ssh -N -L 9090:127.0.0.1:9090 -L 9093:127.0.0.1:9093 root@<monitor_ipv4>
```

**Ingestão de API:** quando o coletor (#30) for publicado na produção, ponha a URL pública dele em `monitor_api_url` e rode o playbook de novo.

## O que o guia ainda não cobre

- **Coletor de API e rollup não estão em produção.** O `prod.yaml` não tem os serviços `collector` ([#30](https://github.com/romulospl/notify/issues/30)) nem `rollup` ([#34](https://github.com/romulospl/notify/issues/34)), e o Caddyfile não tem rota OTLP, embora existam no `dev.yaml`. Efeito: o portal deixa criar chaves de ingestão em `/services`, mas nada as recebe; as tabelas `api_metric_*` ficam vazias e a consolidação e a retenção de métricas de API não rodam. O `notify_collector_db_password` é obrigatório só porque o script de criação dos usuários do banco o usa. A release publica as imagens `collector` e `rollup`, mas o deploy ignora ambas. Para ligá-los será preciso acrescentá-los ao `prod.yaml` (com digest e `cosign verify` no playbook), abrir 4317/4318 no firewall e no Caddy, e então definir `monitor_api_url`.
- **Domínio e DNS na IaC:** hoje os registros são criados à mão (ADR 0004).
- **SMTP de produção:** o guia exige um provedor, mas não escolhe nem configura nenhum (SPF, DKIM e DMARC do domínio do `mail_from` ficam com você).
- **Alta disponibilidade:** é uma VM só. Se ela cair, o serviço cai até a recriação (restauração do backup em VM nova, seção "Restaurar o banco").
- **Teste de restauração em produção:** a restauração é testada no CI com S3 local, não contra o bucket real. Faça um ensaio numa VM descartável antes de precisar dela.
- **Atualização do Umami, do Postgres e do Caddy:** só por PR (imagens por digest no `prod.yaml`); não há guia de migração de versão maior do Postgres.

## Como este guia foi conferido

Sem credenciais da Hetzner, de DNS ou de SMTP, foi possível conferir:

- `docker compose -f infra/compose/prod.yaml config` com um `.env` montado a partir do `env.j2` do Ansible (todas as variáveis obrigatórias do Compose vêm de lá);
- `tofu validate` (contêiner `ghcr.io/opentofu/opentofu:1.13.2`) em `infra/opentofu/`;
- `ansible-playbook --syntax-check` de `site.yml` e `monitoring.yml` (ansible-core 2.21) com os arquivos de exemplo;
- os nomes de variáveis, saídas do OpenTofu, scripts (`pnpm umami:service-account`, `operator:create`) e caminhos citados, contra os arquivos do repositório;
- o roteamento do Caddy, as portas, o backup cifrado e a restauração, pelo job `infra` do CI (`infra/compose/test/prod-stack-test.sh`), e os alertas, pelo job `monitoring`.

**Não foi possível conferir** sem credenciais: o `tofu apply` e o `ansible-playbook` de verdade, a emissão do certificado, o envio de e-mail, o `cosign verify` contra imagens reais (ainda não há release publicada) e a visibilidade dos pacotes no GHCR. Os passos do console da Hetzner e do GitHub seguem a documentação dessas ferramentas e podem ter mudado de nome na interface.
