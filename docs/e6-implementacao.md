# E6: o que foi implementado

Registro da implementação do épico [E6 (#6)](https://github.com/romulospl/notify/issues/6), painéis de analytics de API, iniciado em **14/11/2026**. O E6 não tem spec próprio: ele roda dentro do portal e lê as tabelas que o [spec do E5](specs/e5-pipeline-de-api.md) fixou (seções 4 e 5), com o isolamento do [ADR 0003](adr/0003-isolamento-no-portal.md) e da seção 4 do [spec do E2](specs/e2-tenants-e-controle-de-acesso.md).

## 1. Situação

| Issue | O que é | PR | Commit |
|---|---|---|---|
| [#37 (6.1)](https://github.com/romulospl/notify/issues/37) | Painel de API do tenant | — | Em revisão |
| [#38 (6.2)](https://github.com/romulospl/notify/issues/38) | Painel de API global do SCF | — | A fazer |

## 2. #37: painel de API do tenant

**Onde:** `/services/[serviceId]` (`apps/portal/src/app/(private)/services/[serviceId]/`), aba "Overview". As chaves de ingestão, que ficavam nesse endereço, passaram para a aba "Ingest keys", em `/services/[serviceId]/keys`. Os serviços aparecem em "API analytics", em `/dashboard`, e na lista de `/services`.

**O que mostra, para o período escolhido:**

| Parte | Conteúdo |
|---|---|
| Indicadores | Requisições, fração de erros de servidor (5xx), latência p50 e p95 |
| Gráfico de requisições | Colunas por hora, dia ou mês; o tooltip traz também 4xx e 5xx |
| Gráfico de latência | Linhas de p50 e p95 por balde, com legenda e rótulo no fim de cada linha; balde sem requisição é um buraco, não zero |
| Tabela por endpoint | Método e rota normalizada, requisições, 4xx, 5xx, p50 e p95; as 100 rotas com mais requisições |
| Status | Cada código com o nome, o número e a fração do total |

Os dois gráficos têm a tabela equivalente em "Show charts as a table".

**Leitura das métricas:** `getServiceDashboard` em `src/lib/data/api-metrics.ts`. Recebe o `TenantContext` e o ID do serviço, confere que o serviço é do tenant (`getApiService`) e só então lê as métricas, filtrando por `tenant_id` **e** `api_service_id`. Serviço de outro tenant ou arquivado devolve nulo sem nenhuma leitura de métricas.

**De qual tabela vem cada trecho** (`metricSources`):

- o job da #34 consolida, a cada 15 minutos, as horas que terminaram há pelo menos 5 minutos, e os dias a partir das horas;
- com uma hora de folga, o painel lê `api_metric_1d` para os dias fechados antes da última hora consolidada, `api_metric_1h` para as horas fechadas e `api_metric_1m` para o resto (no máximo as duas últimas horas);
- período por hora (24 horas) nunca usa `api_metric_1d`;
- os trechos não se sobrepõem, então nada conta duas vezes.

Uma consulta só, com `GROUPING SETS`, devolve a série, os endpoints, os status e o total. Os percentis usam `api_metric_quantile(api_metric_sum_buckets(...))`, como o spec do E5 manda: somar os histogramas e calcular sobre a soma.

**Escolhas:**

- **Sem variação sobre o período anterior**, ao contrário do painel web. O período anterior não começa no início de um balde (termina "agora menos o período"), e as tabelas consolidadas não dão o trecho parcial. A #37 não pede comparação; se for preciso, ela entra lendo o período anterior de `api_metric_1h`.
- **Erro é 5xx.** O indicador principal é a fração de erros de servidor; os 4xx aparecem na tabela, no tooltip e na lista de status, porque muitas vezes são uso normal (um 404 de conta inexistente, por exemplo).
- **Latência em ms ou s**, com uma casa abaixo de 10 ms. O percentil para em 10 s, o último limite do histograma (D2 do spec do E5), e aparece como "≥ 10 s".
- **Mesmos períodos do painel web** (`src/lib/period.ts`, em UTC) e o mesmo seletor.
- **Endpoint é método mais rota.** O limite de 100 linhas é folgado: o coletor limita a 500 rotas por serviço e dia (D4 do spec do E5).
- **Gráfico de colunas compartilhado:** o gráfico do painel web virou o componente `src/components/column-chart.tsx`, usado pelos dois painéis. O de latência fica em `services/[serviceId]/latency-chart.tsx`. Cores das posições 1 e 2 da paleta da skill `dataviz` (azul e laranja), nos dois modos.
- **Projeto removido** vê só o aviso, sem leitura de métricas (spec do E2, seção 6).

**Testes:**

- `test/api-dashboard.test.ts`:
  - de qual tabela vem cada trecho, inclusive logo depois da meia-noite;
  - a leitura junta 1d, 1h e 1m sem contar nada duas vezes, com linhas de armadilha (minutos e horas que o consolidado já cobre, o dia corrente parcial, outro serviço, outro tenant);
  - percentil com interpolação conferida;
  - página renderizada, com os indicadores, uma coluna e um ponto por balde, os endpoints e os status;
  - serviço vazio, serviço de outro tenant (404 no painel e nas chaves) e projeto removido.
- `test/isolation.test.ts`: dois casos do painel (#21). A vê só o próprio serviço, mesmo com uma linha impossível (serviço de A com o tenant de B), e o operador recebe 404 (D2).
- **Conferido que os testes pegam falhas:** tirar o filtro de tenant de um trecho, deixar o trecho de horas sobrepor o de minutos e deixar o período por hora ler o 1d fazem testes falharem.

**Conferido no navegador** com cerca de 6 mil linhas de teste (30 dias): claro e escuro, desktop e 390px de largura, períodos de 24 horas, 7 e 30 dias, tooltip do gráfico de latência.

## 3. Limites e pendências

- **Se o job da #34 parar**, as horas que ele deixou de consolidar somem do painel, menos as duas últimas, que vêm do `api_metric_1m`. O monitoramento do job entra com as métricas internas da produção (ADR 0005).
- **#38:** o painel global do SCF ainda não existe.
