# rollup

Job de consolidação e retenção das métricas de API (#34, spec do E5, seção 5). Toda a lógica fica em funções SQL da migração `apps/portal/drizzle/0005_api_metric_rollup.sql`; este processo só agenda a chamada.

A cada `ROLLUP_INTERVAL` ele roda, como `stellytics` (dono das tabelas), `api_metric_maintain(ROLLUP_LOOKBACK)`, que:

1. cria as partições mensais do mês corrente e dos dois seguintes (`api_metric_ensure_partitions(2)`);
2. consolida as horas fechadas (terminadas há pelo menos 5 minutos) de `api_metric_1m` em `api_metric_1h`, e os dias dessas horas em `api_metric_1d` (`api_metric_rollup`), substituindo os valores pela soma recalculada (idempotente);
3. apaga as partições vencidas (`api_metric_drop_partitions`): `api_metric_1m` 35 dias depois do fim do mês, `api_metric_1h` e `api_metric_1d` 25 meses depois.

| Variável | Padrão | Uso |
|---|---|---|
| `DATABASE_URL` | (obrigatória) | Banco `stellytics`, como o usuário `stellytics` |
| `ROLLUP_INTERVAL` | `15m` | Intervalo entre execuções (a primeira roda ao subir) |
| `ROLLUP_LOOKBACK` | `48h` | Quanto para trás reconsolidar a cada execução (mínimo `2h`); cobre lotes atrasados e o job parado por até esse tempo |
| `LISTEN_ADDR` | `:8080` | `GET /healthz`: 200 se a última execução bem-sucedida tiver no máximo dois intervalos |

Imagem `scratch`; o healthcheck é `rollup -healthcheck`. Ficando parado mais que `ROLLUP_LOOKBACK`, reconsolide à mão: `SELECT api_metric_rollup(now() - interval '7 days');`.
