# Runbook: incidentes

Serve para os quatro alertas reais do monitoramento externo ([`alerts.yml`](../../infra/monitoring/prometheus/rules/alerts.yml), [ADR 0005](../adr/0005-monitoramento-externo.md)) e para queda do portal ou da ingestão web. O alerta `Watchdog` é um sinal de vida sempre ativo, não é incidente; se ele **parar de chegar** ao serviço externo configurado em `monitor_deadman_url`, o problema é a VM de monitoramento (ver `ProberDown`).

## Triagem inicial (vale para todos)

1. Veja de fora, de qualquer máquina (comandos do passo 6 de [`docs/deploy.md`](../deploy.md)):

   ```bash
   curl -fsS https://<portal_host>/api/auth/ok           # portal: {"ok":true}
   curl -fsS https://<ingest_host>/healthz               # gateway
   curl -fsS "https://<ingest_host>/c?s=00000000-0000-4000-8000-000000000000"
   ```

2. Entre na VM e veja o estado:

   ```bash
   cd /opt/notify/infra/compose
   docker compose -f prod.yaml ps
   df -h /srv/notify
   ```

3. Se nem o SSH entra, a VM caiu: veja o console da Hetzner (reiniciar, ver o volume). O volume em `/srv/notify` sobrevive à VM; sem ele montado, o Docker e o backup não sobem de propósito. Recriar a VM: `docs/deploy.md`, passos 1 e 4, e a restauração só se o volume foi perdido ([backup e restauração](backup-e-restauracao.md)).

## EndpointDown

**Sintomas.** E-mail `<componente> is down`: o alvo (portal, gateway web ou, no futuro, ingestão de API) ficou 1 minuto sem 2xx.

**Diagnóstico.**

- Qual componente? Está no rótulo `component` do e-mail. Se só um caiu, o problema é do serviço dele; se todos caíram, é a VM, o Caddy ou a rede.
- Na VM: `docker compose -f prod.yaml ps` (procure `unhealthy`, `restarting` ou ausente) e `docker compose -f prod.yaml logs --tail 200 <serviço>`.
- Cadeia de dependências do `prod.yaml`: `caddy` depende de `portal` e `ingest-web`; `ingest-web` depende de `postgres`, `umami` e `portal`; `portal` e `umami` dependem de `postgres`. Comece pelo mais fundo que esteja ruim (normalmente `postgres`: `df -h /srv/notify`, disco cheio derruba tudo).
- Pelo Prometheus da VM de monitoramento (túnel `ssh -N -L 9090:127.0.0.1:9090 -L 9093:127.0.0.1:9093 root@<monitor_ipv4>`, depois `http://127.0.0.1:9090`): a consulta `probe_success` e a página *Status → Targets* mostram qual alvo falha.

**Ação.**

- Serviço parado ou `unhealthy`: `docker compose -f prod.yaml up -d --wait` (recria o que faltar) ou `docker compose -f prod.yaml restart <serviço>`.
- Imagem nova que quebrou: rollback pelos digests anteriores em `group_vars/all.yml` e `ansible-playbook site.yml -e @secrets.yml` (seção "Voltar atrás" de `docs/deploy.md`; o rollback não desfaz migração de banco).
- Disco cheio: veja `docker system df` e `du -sh /srv/notify/*`; sobras de restauração (`postgres.antes-*`) e imagens antigas (`docker image prune`, só depois de conferir) são os candidatos. Aumentar o volume é pela Hetzner.
- Portal sobe mas o painel não mostra dados ou não cria sites: veja se o Umami responde (`docker compose -f prod.yaml ps umami`) e se a `UMAMI_API_KEY` é válida (ver [rotação de segredos](rotacao-de-segredos.md), efeito do `umami_app_secret`).
- Gateway responde `204` mas os eventos não aparecem no painel: o `204` é a resposta também para site recusado e para prova inválida (de propósito). Confira o site na tela do projeto (ativo e não arquivado) e `docker compose -f prod.yaml logs ingest-web`.

**Verificação.** A mesma triagem de fora responde; `docker compose -f prod.yaml ps` com tudo `healthy`; o e-mail de resolvido chega (o Alertmanager envia o `resolved`; ver `alertmanager.yml.j2`, não conferido).

**Perda de dados.** Enquanto o `ingest-web` ou o Umami ficam fora, os eventos web chegados são descartados; não há fila. Registre a janela do incidente.

**Escalar** se: o disco ou a VM têm defeito do provedor (suporte da Hetzner); o Postgres não sobe e o log indica corrupção (ver [backup e restauração](backup-e-restauracao.md)); ou o incidente passa de 1 hora sem causa.

## TLSCertificateExpiringSoon

**Sintomas.** O certificado de um alvo vence em menos de 14 dias. O Caddy renova sozinho cerca de 30 dias antes, então a renovação está falhando.

**Diagnóstico.**

- `docker compose -f prod.yaml logs --tail 300 caddy | grep -i -E 'acme|certificate|error'`.
- DNS: `dig +short <portal_host>` e `dig +short <ingest_host>` precisam apontar para a VM (A e AAAA).
- Porta 80 aberta de fora: `nc -vz -w3 <ip-da-vm> 80` (o desafio do Let's Encrypt passa por ela; o firewall da Hetzner abre 80 e 443).
- O volume do Caddy está em `/srv/notify/caddy` e precisa estar montado e gravável.
- Limite do Let's Encrypt (muitas emissões): o log do Caddy diz.

**Ação.** Corrija a causa (DNS, firewall, volume). Depois `docker compose -f prod.yaml restart caddy` para uma nova tentativa imediata.

**Verificação.** `echo | openssl s_client -connect <portal_host>:443 -servername <portal_host> 2>/dev/null | openssl x509 -noout -enddate` mostra data nova; o alerta se resolve (tem `for: 1h`).

**Escalar** se faltarem menos de 3 dias: um certificado vencido derruba o portal e a ingestão para os navegadores. Se o Let's Encrypt estiver fora, aguarde o status deles.

## ProbeFailing

**Sintomas.** `<componente> is not being checked`: o Prometheus não consegue nem pedir o teste ao blackbox para aquele alvo (`up{job="endpoints"} == 0`). O estado real do alvo é desconhecido, e o `EndpointDown` não dispara.

**Diagnóstico.** Na VM de monitoramento (`cd /opt/notify-monitor`): `docker compose ps`, `docker compose logs --tail 100 prometheus blackbox`. Procure erro de módulo inexistente em `blackbox/blackbox.yml` ou de configuração depois de um deploy do `monitoring.yml`. Teste o alvo à mão pelo Prometheus (túnel acima) em *Status → Targets*.

**Ação.** Restaure a configuração do repositório: `ansible-playbook monitoring.yml -e @secrets.yml` a partir de `infra/ansible/` (copia as configurações e recarrega com `SIGHUP`). Enquanto isso, faça a triagem de fora à mão.

**Verificação.** *Status → Targets* com o alvo `UP` e `probe_success` igual a `1`.

**Escalar** se o playbook falhar de forma repetida: o monitor é a única vigilância externa.

## ProberDown

**Sintomas.** `The blackbox prober is down`: o blackbox exporter não responde e nenhum alvo é testado.

**Diagnóstico.** Na VM de monitoramento: `docker compose ps` (serviços `prometheus`, `blackbox` e `alertmanager`), `docker compose logs --tail 100 blackbox`. Se a VM nem responde ao SSH, o alerta não chegaria (o Alertmanager mora nela): a pista, nesse caso, é o **sinal de vida externo** parar de chegar (`monitor_deadman_url`, opcional; sem ele ninguém fica sabendo).

**Ação.** `docker compose up -d --wait` na pasta do monitor; se a VM caiu, reinicie pelo console da Hetzner (a pilha tem `restart` configurado; não conferido após reboot real). Para recriar do zero, `ansible-playbook monitoring.yml -e @secrets.yml`.

**Verificação.** `probe_success` volta a `1` e `ProberDown` resolve.

**Escalar** se a VM do monitor estiver perdida: a produção continua, mas sem vigilância; recrie com o `tofu apply` e o playbook de monitoramento (`docs/deploy.md`, "Monitoramento").

## Não testado

Nenhum destes procedimentos foi executado numa produção. O disparo real dos alertas foi coberto só pelo teste do CI (`infra/monitoring/test/`, job `monitoring`). O envio de e-mail, o `resolved` e o reinício após reboot não foram conferidos.
