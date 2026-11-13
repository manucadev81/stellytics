# apps/portal

Portal em Next.js (App Router), com Drizzle ORM, Better Auth e Tailwind. É a única porta de entrada da plataforma: cadastro self-service, login, painéis do tenant e painéis globais do SCF.

Lê as estatísticas web pela API do Umami (com uma conta de serviço, [#58](https://github.com/romulospl/notify/issues/58)) e as métricas de API direto do banco `notify`. O isolamento entre tenants na leitura é responsabilidade do portal: o tenant vem sempre da sessão ([#18](https://github.com/romulospl/notify/issues/18)).

## O que já existe ([#57](https://github.com/romulospl/notify/issues/57))

- Login por e-mail e senha com sessão no servidor (Better Auth, tabelas no banco `notify`).
- Rotas privadas: `/dashboard` (área do tenant) e `/operator` (área do operador SCF, só para o papel `operator`; outros recebem 404).
- O `proxy.ts` redireciona para `/sign-in` quem não tem cookie de sessão; a validação de verdade é feita no servidor (`src/lib/session.ts`).
- O cadastro pela API está desligado até o self-service ([#17](https://github.com/romulospl/notify/issues/17)) e a verificação de e-mail ([#60](https://github.com/romulospl/notify/issues/60)). Contas são criadas pelo script abaixo.
- O IP de quem entra no portal não é gravado na sessão; ele só é usado em memória para o limite de tentativas.

A interface é em inglês, porque o público são os projetos do ecossistema Stellar e o SCF.

## Rodando

Pelo Docker Compose, na raiz do repositório (aplica as migrações na subida):

```bash
docker compose up -d --wait
```

O portal fica em http://localhost:3000. Para desenvolver com recarga automática, suba só o banco e rode o `next dev`:

```bash
docker compose up -d --wait postgres
cd apps/portal
DATABASE_URL=postgresql://notify:notify@localhost:5432/notify \
BETTER_AUTH_SECRET=dev-only-portal-auth-secret-change-me \
pnpm db:migrate && pnpm dev
```

### Criar um usuário

A senha vem da variável `PORTAL_USER_PASSWORD` (mínimo de 12 caracteres), para não ficar no histórico do shell:

```bash
cd apps/portal
read -rs PORTAL_USER_PASSWORD && export PORTAL_USER_PASSWORD
pnpm user:create --email voce@example.com --name "Seu nome"
```

Use `--role operator` para um operador SCF. O comando de administração definitivo e o seed de desenvolvimento entram na [#19](https://github.com/romulospl/notify/issues/19).

## Umami ([#58](https://github.com/romulospl/notify/issues/58))

O portal fala com o Umami só por `src/lib/umami/` (o ESLint proíbe importar `src/lib/umami/http.ts` de outro lugar):

- `createSite(ctx, …)` em `src/lib/data/sites.ts` cria o site no Umami e depois grava a linha em `site`; se a gravação falhar, apaga o site no Umami.
- `getSiteStats(ctx, siteId, …)` recebe o ID do site do portal, confere que é do tenant do contexto e só então chama o Umami com o `umami_website_id`. Site de outro tenant devolve nulo sem nenhuma chamada ao Umami.
- `findUmamiInconsistencies()` lista sites do portal que sumiram do Umami e sites do Umami sem linha em `site`.

O portal cria só sites no Umami, nunca usuários nem equipes (ADR 0003). O contexto do tenant (`TenantContext`, em `src/lib/access.ts`) vem da sessão; a #18 completa a camada de dados e a regra de lint para o banco.

### Conta de serviço

Um usuário comum do Umami (`notify-portal`, papel `user`), autenticado por chave de API. O script abaixo cria o usuário (ou troca a senha dele por uma aleatória, que ninguém guarda) e imprime uma chave nova. A senha de administrador só é usada aqui:

```bash
cd apps/portal
read -rs UMAMI_ADMIN_PASSWORD && export UMAMI_ADMIN_PASSWORD
UMAMI_URL=http://127.0.0.1:3001 pnpm umami:service-account
```

Ponha a chave em `UMAMI_API_KEY` (no `.env` da raiz para o Compose) e recrie o portal (`docker compose up -d portal`). Para trocar a chave, rode de novo, atualize o portal e depois rode com `--revoke-old`, que deixa só a chave mais nova.

Use `127.0.0.1`, não `localhost`: se outro programa escutar a mesma porta em IPv6, `localhost` cai nele.

## Variáveis de ambiente

| Variável | Obrigatória | Uso |
|---|---|---|
| `DATABASE_URL` | Sim | Conexão com o banco `notify` |
| `BETTER_AUTH_SECRET` | Sim | Segredo das sessões (gere com `openssl rand -base64 32` em produção) |
| `BETTER_AUTH_URL` | Não | URL pública do portal (padrão `http://localhost:3000`) |
| `RUN_MIGRATIONS` | Não | `true` aplica as migrações do Drizzle na subida do servidor |
| `UMAMI_URL` | Não | Endereço interno do Umami (padrão `http://127.0.0.1:3001`; no Compose, `http://umami:3000`) |
| `UMAMI_API_KEY` | Para sites | Chave de API da conta de serviço. Sem ela o portal sobe, mas não cria sites nem lê estatísticas |

## Banco e migrações

- Esquema em `src/lib/db/schema.ts`; migrações versionadas em `drizzle/`.
- O modelo de tenants (`tenant`, `tenant_member`, `site`, `api_service`, `ingest_key` e `audit_log`) segue a seção 3 do [spec do E2](../../docs/specs/e2-tenants-e-controle-de-acesso.md). O `audit_log` só aceita inserções (um gatilho recusa `UPDATE`, `DELETE` e `TRUNCATE`).
- As visões `ingest_site` (gateway web) e `ingest_key_active` (coletor) e as permissões dos usuários `notify_ingest` e `notify_collector` estão em `drizzle/0002_ingest_views.sql`, escrito à mão. Para SQL que o Drizzle não gera: `pnpm db:generate --custom --name <descricao>`.
- **As migrações exigem os usuários `notify_ingest` e `notify_collector`**, que só um superusuário cria. No Compose, `infra/compose/postgres/init/02-ingest-roles.sh` roda com o volume vazio. Para um volume que já existe, sem apagar os dados:

  ```bash
  docker compose exec -T postgres sh < infra/compose/postgres/init/02-ingest-roles.sh
  ```

- Depois de mudar o esquema: `pnpm db:generate --name <descricao>` e commite o SQL gerado.
- `pnpm db:migrate` aplica as migrações no banco de `DATABASE_URL`.

## Testes

`pnpm test` roda os testes com Vitest. Os de integração (`test/auth.test.ts`, `test/tenant-model.test.ts` e `test/umami.test.ts`) usam o Postgres de `DATABASE_URL` (padrão: o do Compose local) e aplicam as migrações antes de rodar. Os testes de permissão assumem os papéis de ingestão com `SET ROLE`, por isso o script acima também dá ao usuário `notify` o direito de assumi-los (sem herdar nada deles). Os testes do Umami usam um Umami falso (`test/fake-umami.ts`); o job `compose` do CI confere a conta de serviço contra o Umami de verdade.
