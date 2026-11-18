# Runbooks de operação

Procedimentos para operar a produção do stellytics ([#53](https://github.com/romulospl/stellytics/issues/53)). Eles partem do que existe na `main`: o [`infra/compose/prod.yaml`](../../infra/compose/prod.yaml), os scripts de [`infra/`](../../infra/README.md), os alertas de [`infra/monitoring`](../../infra/monitoring/prometheus/rules/alerts.yml) e os comandos `pnpm` do portal. O passo a passo de montar e restaurar a produção continua em [`docs/deploy.md`](../deploy.md); os runbooks apontam para ele em vez de copiá-lo.

> **Status.** Nenhum destes runbooks foi exercitado numa produção real: ainda não há release publicada nem deploy numa conta da Hetzner (ver "Como este guia foi conferido" em [`docs/deploy.md`](../deploy.md)). Os comandos foram conferidos contra os arquivos do repositório (nomes de serviços, scripts, units e variáveis), não executados na VM. Cada runbook lista o que não pôde ser testado. Depois do primeiro incidente ou ensaio real, corrija o runbook.

| Runbook | Quando usar |
|---|---|
| [Incidentes](incidentes.md) | Alerta do monitoramento, portal ou ingestão web fora do ar, certificado vencendo |
| [Backup e restauração](backup-e-restauracao.md) | Backup não rodou, perda de dados ou do disco, ensaio de restauração |
| [Failover manual do banco](failover-do-banco.md) | Só com a réplica ligada (ADR 0009, degrau 2): primário perdido, promover a réplica |
| [Rotação de segredos](rotacao-de-segredos.md) | Segredo vazou ou é hora de trocar |
| [Atualização do Umami](atualizacao-do-umami.md) | PR do Renovate com versão nova do Umami |
| [Resposta a vulnerabilidades](vulnerabilidades.md) | Relato em `SECURITY.md`, chave vazada, projeto a bloquear, operador comprometido |

## Convenções

- **Na VM** quer dizer `ssh root@<ip-da-vm>` e, a partir daí, `cd /opt/stellytics/infra/compose`. Os serviços do `prod.yaml` são `postgres`, `umami`, `portal`, `ingest-web`, `collector`, `rollup` e `caddy` (o projeto se chama `stellytics`; os contêineres são `stellytics-<serviço>-1`). Todos esses serviços, inclusive o proxy Caddy, rodam em Docker Compose.
- **Pelo túnel** quer dizer com [`infra/scripts/admin-tunnel.sh <ip-da-vm>`](../../infra/scripts/admin-tunnel.sh) aberto: Postgres em `127.0.0.1:15432` e Umami em `http://127.0.0.1:13001`.
- **Na VM de monitoramento** os arquivos ficam em `/opt/stellytics-monitor` (`monitor_root`), com `docker compose` a partir dessa pasta.
- Logs de serviço: `docker compose -f prod.yaml logs --tail 200 <serviço>`. O Caddy remove IP, cabeçalhos e URL dos logs; os demais serviços também não devem persistir IP completo.
- O coletor de API e o `rollup` estão no `prod.yaml` desde 16/11/2026 (ADR 0012), mas os runbooks ainda não têm procedimento próprio para eles: use a triagem geral de `incidentes.md` (`ps`, `logs collector`, `logs rollup`).
- Quem escala: hoje a operação é de uma pessoa só (a que mantém o repositório). "Escalar" nos runbooks quer dizer registrar o incidente por escrito (relato de segurança do GitHub, se envolver vulnerabilidade, ou uma nota datada) e acionar suporte do provedor quando o problema for dele (Hetzner, Let's Encrypt, SMTP).
