# services/collector

Distribuição própria do OpenTelemetry Collector, montada com o OCB (OpenTelemetry Collector Builder). Contrato em [`docs/specs/e5-pipeline-de-api.md`](../../docs/specs/e5-pipeline-de-api.md).

- **Pipeline:** receiver OTLP (gRPC na 4317, HTTP na 4318) → `memory_limiter` → `tenantattr` → `batch` → `notifypg` (traces e métricas) ou `nop` (logs).
- **Guarda só contadores e histogramas** em `api_metric_1m`. Traces, spans e logs nunca são gravados (#31).

## Métricas (#31)

O exporter `notifypg` (`components/pgexporter`) usa o pacote `components/httpmetrics` para transformar cada lote em pontos de 1 minuto. Depois grava os pontos em `api_metric_1m` com `INSERT ... ON CONFLICT` somando, numa transação e em ordem fixa de chave.

- **Entradas:** spans de servidor HTTP (`span.kind = SERVER` com método) e o histograma `http.server.request.duration` (ou o antigo `http.server.duration`, em ms). Todo o resto é descartado.
- **Atributos:** de cada requisição ficam só rota normalizada (`routenorm`), método, status e duração. Os buckets de origem são levados para os 15 do banco.
- **D9:** se o serviço mandou a métrica nos últimos 15 minutos, os spans dele são ignorados. Métricas e traces passam por batches separados, então na primeira troca pode haver dupla contagem por um lote.
- **Cumulativo:** pontos `cumulative` viram delta com o último valor de cada série, em memória. A primeira observação só conta se o processo começou há até 5 minutos.
- **Teto de rotas (D4):** 500 rotas por serviço e dia; o excesso vira `{other}`. É contado por instância do coletor.
- **Falha no banco:** o lote é calculado uma vez e regravado até 4 vezes. Depois disso é descartado com erro permanente, porque recalcular a partir do OTLP daria delta zero nas séries cumulativas.
- **Componentes próprios** em [`components/`](components/).

## Autenticação (#30)

Toda requisição OTLP precisa de `Authorization: Bearer ntf_<prefixo>_<segredo>` (spec do E2, seção 5), em HTTP e em gRPC.

1. A extensão `ingestkeyauth` busca a chave pelo prefixo na visão `ingest_key_active`, com o usuário `notify_collector`, e compara o SHA-256 em tempo constante.
2. Chave ausente, mal formada, errada, revogada, de serviço arquivado ou de tenant que não está ativo recebe `401` (HTTP) ou `Unauthenticated` (gRPC). A resposta é a mesma em todos os casos.
3. As respostas, positivas e negativas, ficam em cache por `INGEST_KEY_CACHE_TTL` (no máximo 60 s, 5 s no Compose). Uma revogação vale em até esse tempo. Erro do banco não fica em cache.
4. O processor `tenantattr` apaga do recurso o que o cliente mandou como tenant e grava `notify.tenant.id` e `notify.api_service.id` com os valores da chave. Ele vem antes do `batch`, que perde o contexto do pedido. Dado que chegue sem autenticação é recusado ali.
5. A chave completa nunca vai para log; só o prefixo.

## Código gerado

O manifesto é [`builder-config.yaml`](builder-config.yaml), com os componentes do core do Collector v0.164.0 (core v1.70.0) e os nossos. O OCB sempre cria um `go.mod` próprio. Como o ADR 0002 pede um módulo Go só em `services/`, o [`generate.sh`](generate.sh) gera numa pasta temporária e copia só os `.go` para `cmd/notify-collector`. Depois de mudar o manifesto ou a versão do OCB:

```bash
cd services && collector/generate.sh && go mod tidy
```

O job `go` do CI confere que o código gerado está commitado. Ao subir a versão do Collector, todos os módulos `go.opentelemetry.io/collector/...` precisam ir juntos, na mesma release e com pelo menos 14 dias.

## Rodar

```bash
cd services && go build -o /tmp/notify-collector ./collector/cmd/notify-collector
DATABASE_URL=postgresql://notify_collector:notify_collector@127.0.0.1:5432/notify /tmp/notify-collector --config collector/config.yaml
```

No Compose, o serviço `collector` publica 4317 e 4318 em `127.0.0.1`. A imagem é `scratch`; o healthcheck do contêiner é o binário `/healthcheck`, que faz um GET no receiver HTTP.

## Testes

- `components/ingestkeyauth`: unidade (formato, cache, erro do banco) e integração com a visão real como `notify_collector` (precisa de `DATABASE_URL`; veja `internal/pgtest`).
- `components/tenantattr`: inclui `pipeline_test.go`, que sobe o receiver OTLP de verdade em processo e confere HTTP e gRPC: recusa sem chave e tenant e serviço vindos da chave, por cima do que o cliente mandou.
- `components/httpmetrics`: spans e histogramas para pontos, remapeamento de buckets e ms, cumulativo para delta, D9, teto de rotas.
- `components/pgexporter`: retentativa sobre o mesmo lote e upsert real em `api_metric_1m` como `notify_collector`.
- `e2e/` (tag `e2e`, job `compose` do CI): contra o contêiner, com chaves criadas no banco.
  - Passam a chave válida; são recusadas a errada, a ausente e a de tenant removido.
  - A métrica de um serviço é gravada, e os spans dele são ignorados (D9).
  - Os spans de outro serviço viram rota normalizada.
  - Logs e spans de cliente não deixam nada.
