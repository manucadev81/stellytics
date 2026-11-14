# services/collector/components

Componentes Go do coletor, um pacote por componente:

| Componente | Tipo | Papel |
|---|---|---|
| Autenticação por chave | extension | Valida a chave de ingestão e define o `tenant.id`. Nunca confia em atributo enviado pelo cliente |
| Normalização de rotas | processor | Regras em [`routenorm/`](routenorm/) (#32): `{uuid}`, `{address}` (StrKey G/C/M), `{hash}` (16+ hex) e `{id}`, sem barra final. Um `http.route` enviado pelo cliente tem prioridade. O processor que usa o pacote entra na #31 |
| PostgreSQL | exporter | Grava contadores e histogramas no banco `notify` |
