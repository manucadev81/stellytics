# services/collector

Distribuição própria do OpenTelemetry Collector, montada com o OCB (OpenTelemetry Collector Builder). Contrato em [`docs/specs/e5-pipeline-de-api.md`](../../docs/specs/e5-pipeline-de-api.md).

- **Pipeline:** receiver OTLP (gRPC na 4317, HTTP na 4318) → `memory_limiter` → `tenantattr` → `batch` → exporter. Até a #31 o exporter é o `nop`, então nada é gravado ainda.
- **Guarda só contadores e histogramas.** Traces, spans e logs são descartados (#31).
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
- `e2e/` (tag `e2e`, job `compose` do CI): contra o contêiner, com chaves criadas no banco. Passam a chave válida; são recusadas a errada, a ausente e a de tenant removido.
