# stellytics

Plataforma open source e multi-tenant de **analytics web e de API** para projetos do ecossistema [Stellar](https://stellar.org), proposta para o RFP *Web and API Analytics for Stellar Ecosystem Projects* do [Stellar Community Fund](https://communityfund.stellar.org) (SCF).

> **Estado:** o código do MVP está implementado e testado no CI: portal, script de rastreamento, gateway web, coletor de API, consolidação das métricas, painéis do tenant e do SCF e a infraestrutura de produção como código. **Nenhuma produção real foi aplicada ainda** e não há pilotos; o que falta está em [`docs/andamento.md`](docs/andamento.md).

## O problema

O SCF não tem uma fonte confiável e unificada de dados de uso dos projetos que financia. Hoje os revisores dependem de números informados pelos próprios projetos para decidir quem entra em programas de continuidade.

O stellytics oferece uma plataforma compartilhada e gratuita em que:

- cada projeto (tenant) mede o uso do seu site e da sua API e vê só os próprios dados;
- operadores do SCF veem e comparam as métricas de todos os projetos.

## O que a plataforma mede

**Web**, coletando só no frontend, sem cookies e sem dado pessoal:

- visualizações de página e visitantes únicos;
- referenciadores;
- duração da sessão e taxa de rejeição;
- país, dispositivo e navegador;
- eventos customizados opcionais (`window.stellytics.event`), sem endereço de carteira, hash ou valor.

**API**, a partir do OpenTelemetry que o projeto já envia ao próprio APM (dual-export):

- volume de requisições por endpoint normalizado;
- distribuição de status code;
- latência p50 e p95.

O coletor descarta traces, spans e logs e guarda só contadores e histogramas.

## Arquitetura

```mermaid
flowchart LR
  subgraph Tenant
    B[Navegador<br/>script de versão fixa]
    A[API do tenant<br/>OpenTelemetry]
  end

  subgraph stellytics
    G[Gateway de ingestão web<br/>Go]
    U[Umami<br/>headless]
    C[Coletor OTel<br/>Go]
    R[Rollup<br/>Go]
    P[Portal<br/>Next.js]
    PG[(PostgreSQL)]
  end

  B -->|evento + prova de JS| G
  G -->|IP truncado| U
  A -->|OTLP| C
  U --> PG
  C -->|contadores e histogramas| PG
  R -->|1 min → 1 h → 1 dia, retenção| PG
  P --> U
  P --> PG
  T[Tenant e operador SCF] --> P
```

| Componente | Stack | Papel |
|---|---|---|
| Portal | Next.js | Cadastro self-service (com aprovação prévia opcional), painéis do tenant, painéis globais do SCF e API somente leitura para o SCF (`/api/v1`) |
| Script de rastreamento | TypeScript | Versão fixa e imutável (`/s/vX.Y.Z/n.js`), com SRI e nome que ad blockers não bloqueiam |
| Gateway de ingestão web | Go | Anti-bot (limite de taxa, prova de execução de JS, limite de fingerprints por IP) e truncamento do IP |
| [Umami](https://umami.is) v3.4.1 | Next.js | Motor de analytics web, usado sem fork |
| Coletor de API | OpenTelemetry Collector + componentes Go | Autenticação por chave, normalização de rotas e gravação das métricas |
| Rollup | Go | Consolida as métricas de API de 1 minuto em 1 hora e 1 dia e aplica as retenções (métricas de API e eventos web do Umami) |
| Banco | PostgreSQL 17 | Bancos separados `umami` (analytics web) e `stellytics` (tenants, chaves e métricas de API, com 24 meses de agregados) |
| Infra | OpenTofu, Ansible, Docker Compose, Caddy | Hospedagem na União Europeia (Hetzner), backup contínuo cifrado, recriável a partir deste repositório |

Em produção, o Caddy é a única porta de entrada, com três hosts na 443: portal, ingestão web e ingestão de API (OTLP por HTTP e gRPC). O Umami nunca é exposto.

A visão completa, com os dois fluxos, está em [`docs/arquitetura.md`](docs/arquitetura.md). As decisões e as alternativas descartadas estão nos ADRs ([índice](docs/adr/README.md)); os de base são:

- [0001 — Umami como ferramenta base](docs/adr/0001-ferramenta-base.md)
- [0002 — Arquitetura e stack](docs/adr/0002-arquitetura-e-stack.md)
- [0003 — Isolamento entre tenants é responsabilidade do portal](docs/adr/0003-isolamento-no-portal.md)
- [0004 — Hospedagem na Hetzner, OpenTofu e backup com WAL-G](docs/adr/0004-hospedagem-e-backup.md)

## Documentação

| Para | Leia |
|---|---|
| Integrar um projeto (script, dual-export, painéis) | [`docs/onboarding-de-tenant.md`](docs/onboarding-de-tenant.md) e [`examples/`](examples/README.md) |
| Subir e operar uma instância | [`docs/deploy.md`](docs/deploy.md) e os [runbooks](docs/runbooks/README.md) |
| Privacidade e dados coletados | [`docs/privacidade.md`](docs/privacidade.md) |
| Anti-bot e cadeia de suprimentos | [`docs/anti-bot-e-cadeia-de-suprimentos.md`](docs/anti-bot-e-cadeia-de-suprimentos.md) |
| Disponibilidade e limites atuais | [`docs/sla.md`](docs/sla.md) e o [relatório de carga](docs/relatorios/carga-1m-dia.md) |
| Andamento e pendências | [`docs/andamento.md`](docs/andamento.md) |

## Estrutura do repositório

```
apps/
  portal/            Portal Next.js: cadastro, painéis do tenant e do SCF
packages/
  tracker/           Script de rastreamento (TypeScript, esbuild)
services/            Módulo Go único (go.mod)
  ingest-web/        Gateway de ingestão web
  collector/         Coletor OpenTelemetry (manifesto do OCB)
    components/      Autenticação por chave, normalização de rotas, exporter Postgres
  rollup/            Consolidação e retenção das métricas de API
  loadtest/          Ferramenta de carga (web e API)
  internal/          Código compartilhado dos testes Go (banco com as migrações do portal)
infra/               OpenTofu, Ansible, Docker Compose, Caddy, monitoramento e scripts de operação
examples/            Integrações de referência (HTML, SPA, SSR, backend com OTel)
scripts/             Checagem de licenças e de vulnerabilidades das dependências
docs/                ADRs, specs, guias, runbooks e registro de andamento
```

O portal, o script, cada serviço Go, `infra/` e `examples/` têm um `README.md` próprio.

- **TypeScript:** pnpm workspaces (`apps/*`, `packages/*`, `examples/*`) e Turborepo. O pnpm só instala versões publicadas há pelo menos 14 dias (`minimumReleaseAge` no `pnpm-workspace.yaml`).
- **Go:** um módulo só, em `services/`.

## Rodar localmente

Requer Node.js 24, pnpm 10 (`corepack enable`), Go 1.26 e Docker com Compose v2 e BuildKit.

```bash
pnpm install
pnpm bootstrap    # ou ./scripts/bootstrap.sh
```

O comando de bootstrap prepara o `.env`, sobe o banco e o Umami, gera automaticamente a conta de serviço do portal e inicializa toda a stack.

<details>
<summary>Como fazer o passo a passo manual</summary>

```bash
cp .env.example .env
docker compose up -d --wait   # banco, Umami, portal, gateway, coletor e rollup

# Publica o Umami em 127.0.0.1:3001 (só para desenvolvimento) e gera a chave.
docker compose -f compose.yaml -f infra/compose/umami-port.yaml up -d --wait
UMAMI_ADMIN_PASSWORD=umami UMAMI_URL=http://127.0.0.1:3001 \
  pnpm --filter @stellytics/portal umami:service-account
```

Ponha a chave impressa em `UMAMI_API_KEY`, no `.env` da raiz, e recrie o portal com `docker compose up -d portal`.

</details>

| Serviço | Endereço |
|---|---|
| Portal | http://localhost:3000 |
| Gateway de ingestão web | http://localhost:3002 |
| Coletor de API (OTLP) | http://localhost:4318 (HTTP) e `localhost:4317` (gRPC) |
| Mailpit (e-mails do portal) | http://localhost:8025 |
| PostgreSQL 17 | `localhost:5432` (`stellytics`/`stellytics` e `umami`/`umami`) |

- Portas e senhas: copie `.env.example` para `.env`.
- O Umami não publica porta por padrão; o override `infra/compose/umami-port.yaml` (usado acima) abre a interface dele em http://127.0.0.1:3001. Detalhes em [`docs/umami.md`](docs/umami.md).
- `docker compose down -v` apaga os dados e a próxima subida recomeça do zero.
- Testes: `pnpm lint`, `pnpm typecheck` e `pnpm test` na raiz; para o Go, `go test -race ./...` de dentro de `services/`.

## Deploy em produção

Uma VM na Hetzner (UE) com o Caddy como única porta de entrada. Tudo sai deste repositório, em cinco passos:

| Passo | O que fazer | Ferramenta |
|---|---|---|
| 1. Infraestrutura | Criar a VM, o firewall, o volume e o bucket de backup | `tofu -chdir=infra/opentofu apply` |
| 2. DNS | Apontar três nomes para a VM: portal, ingestão web e ingestão de API | seu provedor de DNS |
| 3. Imagens | Publicar uma release no GitHub; as imagens saem assinadas | workflow `release.yml` |
| 4. Deploy | Preencher o inventário, os segredos e os digests das imagens e aplicar | `ansible-playbook site.yml -e @secrets.yml` |
| 5. Primeira subida | Trocar a senha do Umami, gerar a chave da conta de serviço, criar o primeiro operador do SCF e disparar o primeiro backup | túnel `infra/scripts/admin-tunnel.sh` |

O playbook é idempotente: atualizar a aplicação é trocar os digests e rodar o passo 4 de novo. Ele só sobe imagens por digest e com a assinatura conferida.

O que vem junto: TLS automático, backup contínuo e cifrado do Postgres com restauração testada no CI, e monitoramento externo com alerta por e-mail numa VM separada.

O passo a passo completo, com segredos, restauração e o que ainda não foi testado numa conta real, está em [`docs/deploy.md`](docs/deploy.md); os procedimentos de operação, nos [runbooks](docs/runbooks/README.md).

## CI e cadeia de suprimentos

Todo PR roda o [`ci.yml`](.github/workflows/ci.yml): lint, tipos e testes de TypeScript e Go, os testes ponta a ponta no Compose local, a pilha de produção inteira com um S3 local (rotas do Caddy, backup e restauração) e a pilha de monitoramento. O check a exigir é o agregador `ci`.

- **Dependências travadas** por lockfile, e só releases com pelo menos 14 dias (pnpm e [Renovate](renovate.json)).
- **Licenças e vulnerabilidades** conferidas pelo [`dependencies.yml`](.github/workflows/dependencies.yml); só licenças aprovadas pela OSI.
- **Ad blockers:** o [`adblock.yml`](.github/workflows/adblock.yml) confere todo dia se as listas de bloqueio mais usadas deixam passar o script e as rotas de ingestão.
- Esses dois workflows ficam fora do agregador `ci`, porque o resultado deles muda sem mudança no código.
- **Release:** cada imagem vai para `ghcr.io/romulospl/notify/<nome>`, assinada com cosign e com SBOM atestado.

Para verificar uma imagem publicada:

```bash
cosign verify ghcr.io/romulospl/notify/<nome>:<versão> \
  --certificate-identity-regexp '^https://github.com/romulospl/notify/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Os detalhes estão em [`docs/anti-bot-e-cadeia-de-suprimentos.md`](docs/anti-bot-e-cadeia-de-suprimentos.md).

## Privacidade

A meta é conformidade com a GDPR **sem banner de consentimento**:

- **IP:** o último octeto do IPv4 é descartado (no IPv6 fica só o /48) antes da geolocalização. O IP completo nunca é gravado, nem em log.
- **Navegador:** nenhum cookie, `localStorage` ou identificador persistente.
- **Umami:** gravação de sessão e heatmaps ficam desligados.
- **API:** traces, spans e logs são descartados no coletor; ficam só contadores e histogramas por rota normalizada.
- **Retenção:** eventos web no Umami ficam cerca de seis meses (183 dias, aplicada pelo job de consolidação); métricas de API, 35 dias no detalhe e 25 meses nos agregados.
- **Apagamento (direito à erasure):** quem tem conta no portal exclui a própria conta em `/settings`, com e-mail de confirmação — isso apaga o projeto do usuário, os sites no Umami e as métricas de API, e anonimiza as ações da pessoa no histórico de auditoria. O operador SCF pode apagar os dados de um projeto removido. O mesmo job aplica a retenção automática dos eventos web.
- **Residência:** todos os dados ficam na União Europeia — a infraestrutura fixa Hetzner Nuremberg (`nbg1`), Alemanha, e backups na mesma região, cifrados.
- **Jurisdições:** a lista provisória de jurisdições onde pode ser exigido opt-in (UE/EEE, Alemanha, França, Reino Unido) está em [`docs/privacidade.md`](docs/privacidade.md), seção 8, à espera de assessoria jurídica.

O detalhe, com retenção, fluxos de apagamento e lacunas conhecidas, está em [`docs/privacidade.md`](docs/privacidade.md).

## Roadmap

O código de E1 a E6 está implementado; os guias do E7 estão escritos, à espera de teste por alguém de fora; o E8 (pilotos) não começou e o E9 está parcial. O backlog está nas [issues](https://github.com/romulospl/notify/issues), organizado em épicos, e no [quadro do projeto](https://github.com/users/romulospl/projects/1).

| Épico | Escopo |
|---|---|
| [E1](https://github.com/romulospl/notify/issues/1) | Fundação do monorepo e infraestrutura |
| [E2](https://github.com/romulospl/notify/issues/2) | Multi-tenant, cadastro e controle de acesso |
| [E3](https://github.com/romulospl/notify/issues/3) | Coleta de analytics web |
| [E4](https://github.com/romulospl/notify/issues/4) | Painéis de analytics web |
| [E5](https://github.com/romulospl/notify/issues/5) | Pipeline de analytics de API (OpenTelemetry) |
| [E6](https://github.com/romulospl/notify/issues/6) | Painéis de analytics de API |
| [E7](https://github.com/romulospl/notify/issues/7) | Documentação, privacidade e conformidade |
| [E8](https://github.com/romulospl/notify/issues/8) | Pilotos com projetos reais do SCF |
| [E9](https://github.com/romulospl/notify/issues/9) | Pós-MVP: SLA, escala, chaves de API e transferência |
| [E10](https://github.com/romulospl/notify/issues/63) | Proposta e submissão ao SCF |

## Contribuindo

O guia completo está no [`CONTRIBUTING.md`](CONTRIBUTING.md), e vulnerabilidades são relatadas como descrito no [`SECURITY.md`](SECURITY.md). Dúvidas e bugs vão para as [issues](https://github.com/romulospl/notify/issues/new/choose), que são o canal público de suporte.

- Commits seguem o [Conventional Commits 1.0.0-beta.4](https://www.conventionalcommits.org/pt-br/v1.0.0-beta.4/), por exemplo `feat(collector): add Stellar address normalization`.
- Decisões de arquitetura novas viram um ADR em [`docs/adr/`](docs/adr/README.md), a partir do [template](docs/adr/template.md).

## Licença

[MIT](LICENSE).
