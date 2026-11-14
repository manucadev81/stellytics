# infra

Infraestrutura recriável a partir deste repositório, com hospedagem na União Europeia.

| Pasta (planejada) | Ferramenta | Papel |
|---|---|---|
| [`compose/`](compose/) | Docker Compose | Ambiente local ([#12](https://github.com/romulospl/notify/issues/12)), incluído pelo `compose.yaml` da raiz, e depois o deploy |
| `terraform/` | Terraform | VM, firewall e DNS |
| `ansible/` | Ansible | Deploy das imagens |
| `caddy/` | Caddy | TLS e proxy reverso |
| `monitoring/` | Prometheus, Grafana, Alertmanager | Saúde da ingestão e dos painéis, alertas |

As pastas são criadas pelas issues que as usam.

## Umami só na rede interna

O Umami não pode ser alcançável de fora ([#62](https://github.com/romulospl/notify/issues/62), [docs/umami.md](../docs/umami.md)): quem chegasse ao `/api/send` dele contornaria o anti-bot e o truncamento de IP do gateway.

- O serviço `umami` não publica porta em nenhum arquivo do Compose usado em deploy. O `compose/umami-port.yaml` publica em `127.0.0.1` e é só para desenvolvimento e CI.

O Caddy, o Terraform e o Compose de produção ainda não existem; eles chegam na [#14](https://github.com/romulospl/notify/issues/14). Quando chegarem, precisam cumprir duas regras, conferidas na revisão da #14:

- **O Caddy não tem rota para o Umami**, nem para a interface nem para a API. Publica só o gateway `ingest-web` (`/e`, `/c` e `/s/`) e o portal.
- O firewall da VM (Terraform) não abre a porta do Umami.
