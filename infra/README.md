# infra

Infraestrutura recriável a partir deste repositório, com hospedagem na União Europeia.

| Pasta (planejada) | Ferramenta | Papel |
|---|---|---|
| `compose/` | Docker Compose | Ambiente local ([#12](https://github.com/romulospl/notify/issues/12)) e deploy |
| `terraform/` | Terraform | VM, firewall e DNS |
| `ansible/` | Ansible | Deploy das imagens |
| `caddy/` | Caddy | TLS e proxy reverso |
| `monitoring/` | Prometheus, Grafana, Alertmanager | Saúde da ingestão e dos painéis, alertas |

As pastas são criadas pelas issues que as usam.
