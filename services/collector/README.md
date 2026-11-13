# services/collector

Distribuição própria do OpenTelemetry Collector, montada com o OCB (OpenTelemetry Collector Builder).

- Pipeline: receiver OTLP (gRPC e HTTP), `memory_limiter`, `batch` e o connector `spanmetrics`.
- Componentes próprios em [`components/`](components/): extensão de autenticação por chave de ingestão (define o `tenant.id`), processor de normalização de rotas e exporter para PostgreSQL.
- Guarda só contadores e histogramas. Traces, spans e logs são descartados.

Issues: [#30](https://github.com/romulospl/notify/issues/30) (endpoint OTLP), [#32](https://github.com/romulospl/notify/issues/32) (normalização de rotas) e as demais do épico [E5](https://github.com/romulospl/notify/issues/5).
