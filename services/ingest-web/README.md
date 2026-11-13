# services/ingest-web

Gateway de ingestão web em Go (`net/http`). Fica entre o script de rastreamento e o Umami, que só é acessível pela rede interna.

- Valida a prova de execução de JS e limita fingerprints por IP (em memória).
- Trunca o IP (último octeto do IPv4, /48 no IPv6) antes de repassar ao Umami pelo header de `CLIENT_IP_HEADER`. O IP completo nunca é persistido.
- Endpoints com nomes neutros, sem termos bloqueados por ad blockers.

Criado na [#61](https://github.com/romulospl/notify/issues/61).
