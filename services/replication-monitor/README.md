# replication-monitor

Expõe em `GET /metrics` (porta 9187, formato Prometheus) só números agregados do slot físico de replicação configurado (#135): se o slot existe, se o standby está em streaming, o atraso de replicação e o WAL retido, em bytes. Não consulta nem expõe nada além disso.

O Prometheus da VM de monitoramento coleta esse endpoint e o Alertmanager alerta: replicação parada, atraso acima de 256 MiB e WAL retido acima de 5 GiB (`infra/monitoring/prometheus/rules/alerts.yml`). O deploy é opt-in, com `monitor_replication_enabled` no Ansible e o override [`infra/monitoring/replication.yaml`](../../infra/monitoring/replication.yaml); a imagem é conferida com cosign e o usuário do banco, `stellytics_replication_monitor`, recebe só o papel `pg_monitor` (`infra/compose/postgres/init/03-replication.sh`). O contexto de operação está no [runbook de failover do banco](../../docs/runbooks/failover-do-banco.md).

| Variável | Padrão | Uso |
|---|---|---|
| `REPLICATION_SLOT` | (obrigatória) | Nome do slot físico a observar (padrão do deploy: `stellytics_standby`) |
| `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE` | padrão libpq | Conexão ao primário; o deploy usa o usuário `stellytics_replication_monitor` |

Imagem `scratch`, sem healthcheck próprio: quando a consulta ao primário falha, o endpoint responde `stellytics_replication_scrape_success 0`, que o alerta trata como falha de coleta.
