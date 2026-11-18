# 0014. Infraestrutura de produção em Docker Compose

- **Status:** Aceito
- **Data:** 18/11/2026
- **Issue:** —

## Contexto

A produção precisa ser instalável em uma VPS que ofereça Docker, sem exigir proxy, gerenciador de certificados ou serviços da aplicação instalados diretamente no host. O ADR 0013 moveu o proxy principal para Nginx no host, o que impedia essa implantação integral por contêineres.

## Decisão

Todos os serviços da aplicação, incluindo o proxy público, rodam em Docker Compose. O Caddy é o único contêiner que publica portas na internet (80 e 443), obtém e renova certificados TLS automaticamente e encaminha tráfego para portal, gateway de ingestão e coletor pela rede Docker interna. Postgres mantém sua publicação limitada ao loopback da VPS para administração por túnel SSH. Umami e rollup não publicam portas.

O estado persistente e os certificados do Caddy ficam em `${STELLYTICS_DATA_DIR:-/srv/stellytics}/caddy`. O firewall continua aceitando apenas SSH administrativo, HTTP e HTTPS. O Ansible instala/configura o Docker, copia o Compose, o Caddyfile e o `.env`, verifica as assinaturas das imagens e inicia a pilha.

Esta decisão substitui o ADR 0013 e retoma o encaminhamento pelo Caddy do ADR 0012. O ADR 0013 permanece como registro histórico.

## Consequências

- DNS dos quatro hosts e as portas 80/443 precisam estar disponíveis antes da primeira subida para que o Caddy emita certificados.
- A instalação e renovação TLS passam a seguir o ciclo do contêiner Caddy; seus dados devem ser preservados em backup e em migrações de VPS.
- O Caddy não grava access logs e filtra campos de requisição nos logs de erro.
- A pilha Compose de CI valida rotas, TLS, exposição de portas, proxy OTLP, backup e restauração. A primeira implantação numa conta real ainda precisa ser acompanhada.
