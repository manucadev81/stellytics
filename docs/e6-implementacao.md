# E6: o que foi implementado

Registro da implementação do épico [E6 (#6)](https://github.com/romulospl/stellytics/issues/6), painéis de analytics de API, iniciado em **14/11/2026**. O E6 não tem spec próprio: ele roda dentro do portal e lê as tabelas que o [spec do E5](specs/e5-pipeline-de-api.md) fixou (seções 4 e 5), com o isolamento do [ADR 0003](adr/0003-isolamento-no-portal.md) e da seção 4 do [spec do E2](specs/e2-tenants-e-controle-de-acesso.md).

## 1. Situação

| Issue | O que é | PR | Commit |
|---|---|---|---|
| [#37 (6.1)](https://github.com/romulospl/stellytics/issues/37) | Painel de API do tenant | [#103](https://github.com/romulospl/stellytics/pull/103) | `55857e0`, `5497c27` |
| [#38 (6.2)](https://github.com/romulospl/stellytics/issues/38) | Painel de API global do SCF | — | Em revisão |

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
- o corte entre `api_metric_1h` e `api_metric_1m` é o menor entre a hora corrente menos uma (folga para lotes atrasados do coletor) e o fim da última hora consolidada do tenant (`max(bucket_start)` do 1h, pelo índice `(tenant_id, bucket_start)`);
- o painel lê `api_metric_1d` para os dias inteiros antes desse corte, `api_metric_1h` para as horas antes dele e `api_metric_1m` para o resto: com o job em dia, as duas últimas horas; com o job parado ou atrasado, tudo o que ele ainda não consolidou, em vez de sumir do painel;
- período por hora (24 horas) nunca usa `api_metric_1d`;
- os trechos não se sobrepõem, então nada conta duas vezes.

Uma consulta só, com `GROUPING SETS`, devolve a série, os endpoints, os status e o total. Os percentis usam `api_metric_quantile(api_metric_sum_buckets(...))`, como o spec do E5 manda: somar os histogramas e calcular sobre a soma.

**Escolhas:**

- **Sem variação sobre o período anterior**, ao contrário do painel web. O período anterior não começa no início de um balde (termina "agora menos o período"), e as tabelas consolidadas não dão o trecho parcial. A #37 não pede comparação; se for preciso, ela entra lendo o período anterior de `api_metric_1h`.
- **Erro é 5xx.** O indicador principal é a fração de erros de servidor; os 4xx aparecem na tabela, no tooltip e na lista de status, porque muitas vezes são uso normal (um 404 de conta inexistente, por exemplo).
- **Latência em ms ou s**, com uma casa abaixo de 10 ms. O percentil para em 10 s, o último limite do histograma (D2 do spec do E5), e aparece como "≥ 10 s".
- **Mesmos períodos do painel web** (`src/lib/period.ts`, em UTC) e o mesmo seletor.
- **Endpoint é método mais rota.** O limite de 100 linhas é folgado: o coletor limita a 500 rotas por serviço e dia (D4 do spec do E5).
- **Teclado:** cada gráfico é uma parada de Tab só (o balde mais recente); as setas, Home e End andam entre os baldes e abrem o tooltip. Sem isso, 90 dias seriam 90 paradas por gráfico. Vale também para o painel web.
- **Frações** nunca arredondam para 0% nem 100% quando não são exatas ("< 0.1%", "> 99%").
- **Gráfico de colunas compartilhado:** o gráfico do painel web virou o componente `src/components/column-chart.tsx`, usado pelos dois painéis. O de latência fica em `services/[serviceId]/latency-chart.tsx`. Cores das posições 1 e 2 da paleta da skill `dataviz` (azul e laranja), nos dois modos.
- **Projeto removido** vê só o aviso, sem leitura de métricas (spec do E2, seção 6).

**Testes:**

- `test/api-dashboard.test.ts`:
  - de qual tabela vem cada trecho, inclusive logo depois da meia-noite, com o job atrasado e sem nada consolidado;
  - com o job parado, as horas que só existem no 1m aparecem, sem contar duas vezes as que já estão no 1h;
  - a leitura junta 1d, 1h e 1m sem contar nada duas vezes, com linhas de armadilha (minutos e horas que o consolidado já cobre, o dia corrente parcial, outro serviço, outro tenant);
  - percentil com interpolação conferida;
  - página renderizada, com os indicadores, uma coluna e um ponto por balde, os endpoints e os status;
  - serviço vazio, serviço de outro tenant (404 no painel e nas chaves) e projeto removido.
- `test/isolation.test.ts`: dois casos do painel (#21). A vê só o próprio serviço, mesmo com uma linha impossível (serviço de A com o tenant de B), e o operador recebe 404 (D2).
- **Conferido que os testes pegam falhas:** tirar o filtro de tenant de um trecho, deixar o trecho de horas sobrepor o de minutos, deixar o período por hora ler o 1d e ignorar a última hora consolidada fazem testes falharem.

**Revisão independente** de um subagente: sem falha de isolamento nem de SQL. Tratados o corte fixo (dados sumiam com o job atrasado), as paradas de Tab, o arredondamento das frações e um trecho do `e2-implementacao.md` que ainda apontava as chaves para `/services/[serviceId]`.

**Conferido no navegador** com cerca de 6 mil linhas de teste (30 dias): claro e escuro, desktop e 390px de largura, períodos de 24 horas, 7 e 30 dias, tooltip do gráfico de latência.

## 3. #38: painel de API global do SCF

**Onde:** `/operator/api` (`apps/portal/src/app/(private)/operator/api/`), com links no cabeçalho do operador ("API analytics") e em `/operator`. Quem não é operador recebe 404.

**O que mostra:** uma linha por projeto **ativo**, com o número de serviços, as requisições (com uma barra proporcional), a fração de 5xx e de 4xx e a latência p50 e p95 de todas as requisições do projeto. Filtro por nome ou slug, ordenação clicando no cabeçalho e o mesmo seletor de período. Tudo por GET, como o painel web do SCF (#29). Um resumo acima da tabela diz quantos projetos mandaram dados no período.

**Para achar serviços pouco usados:** projeto sem serviço aparece como "No API services", e projeto com serviço mas sem requisição, como "No requests in this period". A ordem crescente por requisições põe esses primeiro. Nas colunas de erro e latência, quem não tem requisição fica sempre no fim, nos dois sentidos.

**Leitura:** `getApiOverview` em `src/lib/data/operator/api.ts`.

- Uma consulta traz os projetos ativos, o número de serviços não arquivados e a última hora consolidada de cada um.
- Cada projeto ganha os próprios trechos pela **mesma** `metricSources` do painel do tenant, e eles vão ao banco num JSON (`json_to_recordset`; o driver não serializa arrays de texto nessa consulta).
- Uma segunda consulta soma 1d, 1h e 1m por projeto, só dos serviços não arquivados, e calcula os percentis sobre os histogramas somados.

**Escolhas:**

- **Por projeto, não por serviço**, como a #38 pede. O número de serviços fica na tabela; o detalhe por serviço está no painel do próprio tenant.
- **Serviço arquivado não conta**, como no painel do tenant, em que ele responde 404.
- **Sem variação sobre o período anterior**, pelo mesmo motivo da #37.
- **Projeto removido ou pendente não aparece** (spec do E2, seção 6).

**Testes:** `test/operator-api.test.ts`:

- soma dos serviços, com armadilhas de dupla contagem, dia parcial, hora consolidada dentro da folga, período, serviço arquivado e projeto removido;
- job atrasado num dos projetos;
- **o painel global mostra os mesmos números que a soma dos painéis de cada serviço** (requisições, 4xx e 5xx) em 24 horas, 7 e 30 dias e 12 meses, e os mesmos percentis com um serviço só;
- página: ordem padrão, crescente, por 5xx com os vazios no fim, filtro, valores desconhecidos na URL e 404 para membro de projeto.

A suíte de isolamento (`isolation.test.ts`) confere o 404 do painel para membro de projeto.

**Conferido que os testes pegam falhas:** contar serviço arquivado, ignorar a última hora consolidada, incluir projeto removido e deixar o trecho de horas invadir o de minutos fazem testes falharem. O último só passou a falhar depois de uma linha de armadilha nova (hora consolidada dentro da folga); antes, a mutação sobrevivia.

**Conferido no navegador** com um operador local: desktop e 390px no modo escuro, 7 e 30 dias, ordem crescente. Os números do projeto de teste batem com os do painel dele.

## 4. Limites e pendências

- **Se o job da #34 ficar parado mais de 35 dias** (a retenção do `api_metric_1m`), as horas não consolidadas se perdem. Até lá, o painel lê do `api_metric_1m`. O monitoramento do job entra com as métricas internas da produção (ADR 0005).
- **A tabela de endpoints** traz todos os pares rota e método do período antes de cortar em 100. Com o teto de 500 rotas por serviço e dia, cabe; se crescer, vira uma consulta com `LIMIT`. O monitoramento do job entra com as métricas internas da produção (ADR 0005).
