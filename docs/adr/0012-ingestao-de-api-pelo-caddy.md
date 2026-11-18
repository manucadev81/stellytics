# 0012. Ingestão de API em produção pelo Caddy, na porta 443

- **Status:** Substituído por 0013
- **Data:** 16/11/2026
- **Issue:** #14 (IaC da produção), #30 e #34

## Contexto

O coletor de API (`services/collector`) e o job de consolidação (`services/rollup`) existiam só no Compose de desenvolvimento. Em produção o portal deixava criar chaves de ingestão, mas nada recebia OTLP: as tabelas `api_metric_*` ficavam vazias e a retenção não rodava. Faltava decidir por onde o OTLP entra. A chave de ingestão é um bearer token, então o tráfego precisa de TLS.

## Decisão

O coletor e o rollup entram no `infra/compose/prod.yaml`, sem porta publicada. O OTLP sai pelo Caddy, num terceiro host (`API_HOST`, `api_host` no Ansible), na porta 443:

- OTLP/HTTP: `POST /v1/traces`, `/v1/metrics` e `/v1/logs` vão para `collector:4318`;
- OTLP/gRPC: `POST` com `Content-Type: application/grpc*` em `/opentelemetry.proto.collector.*` vai para `collector:4317` por h2c;
- todo o resto responde 404.

As imagens `collector` e `rollup` vão por digest e passam pelo `cosign verify` do Ansible, como as do portal e do gateway. O contêiner do coletor tem teto de memória (`COLLECTOR_MEM_LIMIT`, padrão 512 MB), porque o `memory_limiter` calcula 80% sobre a memória que enxerga: sem teto, seria a da VM inteira. O monitoramento externo testa o host com um POST sem chave e espera o 401 do coletor (módulo `otlp_401` do blackbox).

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Caddy na 443, host próprio (escolhida) | Um ponto de entrada só; TLS automático; firewall sem mudança; HTTP e gRPC no mesmo endpoint | Um registro de DNS a mais; o Caddy vira ponto único também da API |
| Publicar 4317 e 4318 e abrir no firewall | Portas padrão do OTLP | O coletor teria de terminar TLS (certificado fora do Caddy); mais superfície exposta; firewall e OpenTofu mudam |
| Caminho no host da ingestão web (`INGEST_HOST`) | Sem DNS novo | Mistura tráfego de navegador e de servidor; o host é escolhido para escapar de ad blockers, não para SDKs; gRPC e as rotas do gateway no mesmo host |

## Consequências

- A produção precisa de um terceiro registro de DNS (A e AAAA) antes do deploy; sem ele o Caddy não consegue o certificado desse host (os outros dois seguem funcionando).
- Quem já tem um `.env` de produção precisa de `API_HOST`, `COLLECTOR_IMAGE` e `ROLLUP_IMAGE`; o `prod.yaml` não sobe sem eles.
- O Caddy não limita requisições por IP; o coletor só tem a autenticação por chave e o `memory_limiter`. Limite por chave ou por tenant no coletor fica em aberto.
- Testado só localmente (`infra/compose/test/prod-stack-test.sh`: 401 sem chave em HTTP e gRPC, 404 no resto, métrica gravada no tenant da chave, chave fora dos logs). Nunca aplicado numa conta real; as réplicas de ingestão (`prod-ingest.yaml`) continuam só com o gateway web.
