# services/collector/components

Componentes Go do coletor, um pacote por componente:

| Componente | Tipo | Papel |
|---|---|---|
| Autenticação por chave | extension | Valida a chave de ingestão e define o `tenant.id`. Nunca confia em atributo enviado pelo cliente |
| Normalização de rotas | processor | IDs numéricos, endereços Stellar G-/C-/M-, UUIDs, hashes hex e barra final. Um `http.route` enviado pelo cliente tem prioridade |
| PostgreSQL | exporter | Grava contadores e histogramas no banco `notify` |
