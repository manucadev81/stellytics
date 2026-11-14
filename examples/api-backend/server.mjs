// Backend de exemplo instrumentado com OpenTelemetry (#35).
//
// Mede cada requisição na métrica http.server.request.duration (histograma em
// segundos, convenções semânticas do OTel) e exporta por OTLP/HTTP:
//   - para o backend próprio do tenant (APM ou OTel Collector), em
//     OTEL_EXPORTER_OTLP_ENDPOINT;
//   - e, se NOTIFY_OTLP_ENDPOINT e NOTIFY_INGEST_KEY estiverem definidos,
//     também para o coletor do notify (dual-export direto pelo SDK).
// Sem nenhum dos dois, só serve as rotas.
import http from "node:http";
import { metrics } from "@opentelemetry/api";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-proto";
import { resourceFromAttributes } from "@opentelemetry/resources";
import {
  AggregationTemporality,
  MeterProvider,
  PeriodicExportingMetricReader,
} from "@opentelemetry/sdk-metrics";

const port = Number(process.env.PORT ?? 4010);
const interval = Number(process.env.EXPORT_INTERVAL_MS ?? 10000);

// Endpoint base OTLP/HTTP (sem /v1/metrics).
function reader(endpoint, headers) {
  return new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter({
      url: `${endpoint.replace(/\/$/, "")}/v1/metrics`,
      headers,
      // Delta é o recomendado pelo notify (spec do E5, seção 2).
      temporalityPreference: AggregationTemporality.DELTA,
    }),
    exportIntervalMillis: interval,
  });
}

const readers = [];
if (process.env.OTEL_EXPORTER_OTLP_ENDPOINT) {
  readers.push(reader(process.env.OTEL_EXPORTER_OTLP_ENDPOINT, {}));
}
if (process.env.NOTIFY_OTLP_ENDPOINT && process.env.NOTIFY_INGEST_KEY) {
  readers.push(
    reader(process.env.NOTIFY_OTLP_ENDPOINT, {
      Authorization: `Bearer ${process.env.NOTIFY_INGEST_KEY}`,
    }),
  );
}

const provider = new MeterProvider({
  resource: resourceFromAttributes({
    "service.name": process.env.OTEL_SERVICE_NAME ?? "example-api-backend",
  }),
  readers,
});
metrics.setGlobalMeterProvider(provider);

const duration = provider
  .getMeter("example-api-backend")
  .createHistogram("http.server.request.duration", {
    unit: "s",
    description: "Duration of HTTP server requests.",
    advice: {
      // Limites recomendados pelas convenções do OTel, os mesmos do notify.
      explicitBucketBoundaries: [
        0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5,
        10,
      ],
    },
  });

// Rotas com o padrão já conhecido: o http.route vai pronto, sem IDs.
const routes = [
  { method: "GET", pattern: /^\/accounts\/([A-Z0-9]{56})$/, route: "/accounts/{account_id}" },
  { method: "GET", pattern: /^\/accounts\/([A-Z0-9]{56})\/payments$/, route: "/accounts/{account_id}/payments" },
  { method: "GET", pattern: /^\/ledgers\/(\d+)$/, route: "/ledgers/{sequence}" },
  { method: "GET", pattern: /^\/healthz$/, route: "/healthz" },
];

const server = http.createServer((req, res) => {
  const start = performance.now();
  const path = (req.url ?? "/").split("?")[0];
  const match = routes.find((r) => r.method === req.method && r.pattern.test(path));

  let status = 404;
  let body = { error: "not found" };
  if (match) {
    status = 200;
    body = { ok: true, route: match.route };
  } else if (path === "/fail") {
    status = 500;
    body = { error: "boom" };
  }
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));

  const attrs = {
    "http.request.method": req.method ?? "GET",
    "http.response.status_code": status,
  };
  // Sem padrão conhecido, vai o caminho e o coletor do notify normaliza.
  if (match) attrs["http.route"] = match.route;
  else attrs["url.path"] = path;
  duration.record((performance.now() - start) / 1000, attrs);
});

server.listen(port, () => {
  console.log(`example-api-backend on :${port}, ${readers.length} OTLP destination(s)`);
});

// Exporta o que falta antes de sair.
async function stop() {
  server.close();
  await provider.shutdown();
  process.exit(0);
}
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
