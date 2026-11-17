# api-backend

Backend HTTP de exemplo (Node, sem framework) instrumentado com OpenTelemetry, e guia de **dual-export**: a API continua mandando a telemetria para o APM do próprio projeto e manda a mesma coisa para o coletor do stellytics (#35). Contrato do coletor em [`docs/specs/e5-pipeline-de-api.md`](../../docs/specs/e5-pipeline-de-api.md).

O `server.mjs` mede cada requisição no histograma `http.server.request.duration` (segundos, convenções semânticas do OTel), com `http.request.method`, `http.response.status_code` e `http.route` (ou `url.path`, quando a rota não tem padrão conhecido; o coletor normaliza). Rotas: `/accounts/<G...>`, `/accounts/<G...>/payments`, `/ledgers/<n>`, `/healthz` e `/fail` (500); o resto responde 404.

| Variável | Para quê |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Base OTLP/HTTP do backend próprio (APM ou OTel Collector do tenant) |
| `STELLYTICS_OTLP_ENDPOINT` e `STELLYTICS_INGEST_KEY` | Base OTLP/HTTP do coletor do stellytics e a chave `ntf_...`; com as duas, o SDK faz o dual-export sozinho |
| `OTEL_SERVICE_NAME` | `service.name` (o stellytics ignora: o serviço vem da chave) |
| `PORT`, `EXPORT_INTERVAL_MS` | Porta (padrão 4010) e intervalo de exportação (padrão 10 s) |

## O que o stellytics precisa receber

- **Endpoint:** OTLP/HTTP (`/v1/metrics`, `/v1/traces`) ou OTLP/gRPC do coletor do stellytics. No Compose local, `http://127.0.0.1:4318` (HTTP) e `127.0.0.1:4317` (gRPC).
- **Chave:** `Authorization: Bearer ntf_...`, criada em **Services** no portal. A chave define o projeto e o serviço; atributos mandados pelo cliente não mudam isso. Uma chave por serviço.
- **Sinal:** a métrica `http.server.request.duration` (preferida) ou spans de servidor HTTP. Se o serviço manda as duas coisas, o stellytics usa a métrica e ignora os spans, para não contar duas vezes. Logs e o resto são descartados no coletor.
- **Temporalidade:** prefira **delta**. Cumulativo também funciona (o coletor converte), mas um reinício do coletor pode perder um intervalo.
- **Privacidade:** o stellytics só lê rota, método, status e duração. Mesmo assim, não mande dados pessoais em atributos: o filtro mais seguro é o que roda no seu lado (veja o Collector abaixo).

## Dual-export pelo SDK

Cada destino é um `PeriodicExportingMetricReader` com o próprio exporter, no mesmo `MeterProvider`. É o que o `server.mjs` faz:

```js
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-proto";
import { AggregationTemporality, MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";

const reader = (url, headers = {}) =>
  new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter({ url, headers, temporalityPreference: AggregationTemporality.DELTA }),
  });

const provider = new MeterProvider({
  readers: [
    reader("https://apm.example.com/v1/metrics"), // backend próprio
    reader("https://<coletor-do-stellytics>/v1/metrics", { Authorization: `Bearer ${process.env.STELLYTICS_INGEST_KEY}` }),
  ],
});
```

Para traces, o mesmo vale com dois `BatchSpanProcessor` no `TracerProvider`. Em outras linguagens a ideia é igual: dois exporters OTLP, um com o header `Authorization`. As variáveis padrão `OTEL_EXPORTER_OTLP_*` configuram um destino só; o segundo precisa ser montado no código, ou use o Collector.

Rodar no host, com o Compose no ar e uma chave criada no portal:

```bash
cd examples/api-backend
STELLYTICS_OTLP_ENDPOINT=http://127.0.0.1:4318 STELLYTICS_INGEST_KEY=ntf_... pnpm start
curl http://127.0.0.1:4010/ledgers/1
```

## Dual-export pelo OTel Collector

Se a API já manda para um OTel Collector próprio, basta acrescentar um exporter. O [`otelcol.yaml`](otelcol.yaml) recebe OTLP do app e manda o mesmo dado para o backend próprio (o exporter `debug` fica no lugar dele) e para o stellytics:

```yaml
exporters:
  otlp_http/stellytics:
    endpoint: ${env:STELLYTICS_OTLP_ENDPOINT}
    headers:
      Authorization: Bearer ${env:STELLYTICS_INGEST_KEY}

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug, otlp_http/stellytics] # troque debug pelo exporter do seu APM
```

Funciona com o Collector core (`otel/opentelemetry-collector`) a partir da v0.164.0; em versões mais antigas, o nome do exporter é `otlphttp`. Com o contrib, dá para mandar ao stellytics só o necessário, por exemplo um processor `filter` antes do `otlp_http/stellytics` numa pipeline separada.

## No Compose

O override [`infra/compose/api-example.yaml`](../../infra/compose/api-example.yaml) sobe o app (porta 4010) e o Collector do tenant (`tenant-otelcol`), que faz o dual-export até o coletor do stellytics:

```bash
STELLYTICS_INGEST_KEY=ntf_... docker compose -f compose.yaml -f infra/compose/api-example.yaml up -d --wait
curl http://127.0.0.1:4010/accounts/GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7
docker compose -f compose.yaml -f infra/compose/api-example.yaml logs tenant-otelcol   # "APM próprio"
```

As linhas aparecem em `api_metric_1m` em alguns segundos. O [`compose-check.sh`](compose-check.sh) faz tudo isso sozinho (cria tenant e chave no banco, sobe, faz requisições, confere as linhas e apaga) e roda no job `compose` do CI.
