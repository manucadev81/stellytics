# 0013. Nginx no host como proxy da produção

- **Status:** Substituído por 0014
- **Data:** 18/11/2026
- **Issue:** —

## Contexto

A produção principal precisa encaminhar portal, ingestão web e OTLP. O proxy em contêiner compartilhava a rede Docker e a configuração de produção com serviços que não precisam de acesso público. A nova composição publica os upstreams necessários apenas em loopback.

## Decisão

Usar Nginx instalado no host da VM principal como único proxy público. O Nginx termina TLS com certificados gerenciados pelo Certbot e encaminha para as portas locais do portal, do gateway e do coletor. Docker publica esses upstreams em `127.0.0.1`; Umami, Postgres e rollup continuam sem portas públicas. As VMs extras de ingestão mantêm Caddy.

O operador provisiona DNS e emite o certificado para os quatro nomes antes de executar o playbook Ansible. O Ansible instala Nginx e Certbot, configura o vhost e instala um hook para recarregar o Nginx após renovação.

Esta decisão substitui o proxy Caddy da VM principal descrito no ADR 0004 e o encaminhamento OTLP do ADR 0012. Os ADRs antigos permanecem como registro histórico.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Manter Caddy em contêiner | TLS automático e proxy simples de operar | O proxy fica acoplado à rede e ao ciclo da pilha Docker |
| Nginx em contêiner | Configuração declarativa e ciclo gerenciado pelo Compose | Ainda depende de publicar portas do contêiner e montar certificados no Docker |
| Nginx no host | Upstreams vinculados a loopback e TLS gerenciado pelo Certbot do sistema | Certificado inicial exige uma etapa manual antes do playbook |

## Consequências

- Os serviços públicos só aceitam conexões locais; o firewall da VM mantém abertas apenas 22, 80 e 443.
- O vhost não configura access log; o error log dedicado registra apenas erros críticos.
- A emissão inicial do certificado é uma etapa documentada antes do Ansible. Um hook recarrega o Nginx nas renovações automáticas.
- A configuração do Nginx é validada com `nginx -t` antes de recarregar o serviço.
- A pilha local verifica os upstreams Docker, mas não executa Nginx; a validação do vhost ocorre na VM.
