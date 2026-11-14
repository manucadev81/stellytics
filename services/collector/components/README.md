# services/collector/components

Componentes Go do coletor, um pacote por componente:

| Componente | Tipo | Papel |
|---|---|---|
| Autenticação por chave | extension | [`ingestkeyauth/`](ingestkeyauth/) (#30): valida a chave de ingestão na `ingest_key_active` e põe tenant e serviço no contexto do pedido. Nunca confia em atributo enviado pelo cliente |
| Carimbo de tenant | processor | [`tenantattr/`](tenantattr/) (#30): troca os atributos de tenant do recurso por `notify.tenant.id` e `notify.api_service.id` da chave. Vem antes do `batch` |
| Normalização de rotas | processor | Regras em [`routenorm/`](routenorm/) (#32): `{uuid}`, `{address}` (StrKey G/C/M), `{hash}` (16+ hex) e `{id}`, sem barra final. Um `http.route` enviado pelo cliente tem prioridade. O processor que usa o pacote entra na #31 |
| PostgreSQL | exporter | [`pgexporter/`](pgexporter/) (#31): grava os pontos de 1 minuto em `api_metric_1m` |
| Extração de métricas | pacote | [`httpmetrics/`](httpmetrics/) (#31): spans de servidor HTTP e `http.server.request.duration` viram pontos de 1 minuto; o resto é descartado |
