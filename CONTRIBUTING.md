# Como contribuir

Obrigado pelo interesse no stellytics. Este guia explica como preparar o ambiente, rodar os testes e enviar mudanças.

## Antes de começar

- **Bugs e dúvidas:** abra uma [issue](https://github.com/romulospl/stellytics/issues/new/choose) com o modelo adequado.
- **Vulnerabilidades:** não abra issue pública; siga o [`SECURITY.md`](SECURITY.md).
- **Mudanças grandes ou de arquitetura:** abra uma issue antes de programar. Decisões de arquitetura viram um ADR em [`docs/adr/`](docs/adr/README.md), a partir do [template](docs/adr/template.md). ADR aceito não é reescrito: crie outro e marque o antigo como substituído.

## Ambiente

Requisitos: Node.js 24 ou mais novo, pnpm 10 (`corepack enable`), Go 1.26 e Docker com Compose v2.

```bash
pnpm install                 # dependências TypeScript
docker compose up -d --wait  # Postgres e Umami locais
```

O passo a passo do ambiente local, com portas e acessos, está no [README](README.md#ambiente-local-docker-compose).

## Testes

```bash
pnpm lint
pnpm typecheck
pnpm test
pnpm build
```

Para o código Go, de dentro de `services/`:

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

Testes de integração usam um Postgres real, lido de `DATABASE_URL` (no ambiente local, `postgresql://stellytics:stellytics@localhost:5432/stellytics`). O CI roda tudo isso em cada PR.

Regras que os testes precisam cobrir:

- **Isolamento entre tenants:** todo acesso a dados novo precisa de teste mostrando que um tenant não vê dados de outro.
- **Privacidade:** o IP completo nunca é persistido; nada de cookies ou storage no navegador.
- **Normalização de rotas:** cada regra com casos positivos e negativos.

## Dependências

- Dependências devem usar licença aprovada pela [OSI](https://opensource.org/licenses); exceções específicas e temporárias para ferramentas de build são registradas em `scripts/licenses/allowed.json` e revistas na data indicada.
- Fixe versões exatas e commite o lockfile (`pnpm-lock.yaml`, `services/go.sum`). O CI falha se o lockfile estiver desatualizado.
- Só adote releases com pelo menos 14 dias, salvo a exceção restrita para correções de segurança críticas ou altas descrita no [runbook](docs/runbooks/vulnerabilidades.md). O pnpm aplica `minimumReleaseAge`; o Renovate ainda depende de instalação e dos alertas do GitHub.
- Actions do GitHub fixadas por SHA, com a tag em comentário.

### Alertas de dependências e licenças

O workflow `dependencies` (`.github/workflows/dependencies.yml`) roda nos PRs que mexem em manifestos ou lockfiles, na `main` e todo dia. Ele não faz parte do check `ci`, porque um advisory novo muda o resultado sem mudança no código; um vermelho na `main` precisa de alguém olhando. Para rodar as checagens localmente:

```bash
# de services/: vulnerabilidades alcançáveis. Fixe o Go das imagens: leia a versão
# em `FROM golang:X.Y.Z` dos services/*/Dockerfile (hoje 1.26.9) e use no GOTOOLCHAIN
GOTOOLCHAIN=go1.26.9 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
# da raiz: vulnerabilidades do pnpm (high e critical falham; sai 2 se não consultou; lista também as de devDependencies, sem falhar)
node scripts/deps/audit-pnpm.mjs
# da raiz, depois de pnpm install: licenças
pnpm licenses list --json --prod > /tmp/npm-licenses.json
(cd services && go run github.com/google/go-licenses/v2@v2.0.1 report ./... --template ../scripts/licenses/go-licenses.tpl --ignore github.com/romulospl/stellytics/services) > /tmp/go-licenses.tsv
node scripts/deps/check-licenses.mjs --npm /tmp/npm-licenses.json --go /tmp/go-licenses.tsv
node --test scripts/deps/*.test.mjs   # testes dos conferidores
```

**Vulnerabilidade.** Leia o pacote, a versão corrigida e o caminho (o relatório mostra de quem a dependência vem). Prefira subir a versão, respeitando os 14 dias de espera. Se a correção ainda não tem 14 dias, espere a data em que ela passa a valer (o job `govulncheck` fica vermelho até a imagem subir, o que é esperado e curto) e avalie mitigação nesse intervalo; a regra de 14 dias não é afrouxada. Para uma dependência transitiva, suba a que a traz ou use `pnpm.overrides` com versão exata. `moderate` e `low` do `pnpm audit` não falham o job, mas aparecem no log: trate quando houver correção. Nunca silencie um alerta `high` ou `critical`, nem troque o limite do `audit`, para deixar o job verde. Um job vermelho com "Nao consegui consultar" não é vulnerabilidade: é falha do endpoint ou da rede, rode de novo.

**Licença fora da lista.** A allowlist é `scripts/licenses/allowed.json` (ids SPDX). Expressão com `OR` (dual license) passa se uma alternativa estiver na lista; com `AND`, todas precisam estar; `X WITH exceção` vale como `X`. Sem licença, `UNLICENSED`, `Unknown` e `SEE LICENSE IN ...` falham sempre. O que fazer, em ordem:

1. Trocar a dependência por uma com licença permitida.
2. Se a licença é aprovada pela [OSI](https://opensource.org/licenses) e permissiva, acrescentar o id SPDX em `allowed` no PR, com a justificativa na descrição. Copyleft (LGPL, GPL, AGPL; a MPL-2.0 já está liberada) não entra na lista geral: vira exceção por pacote (`package` pode terminar em `*` para npm, como `@img/sharp-libvips-*`). No caso do libvips, a decisão atual é não distribuí-lo: a otimização de imagens está desativada e sharp/libvips são removidos do standalone. Uma futura distribuição de LGPL exige análise e cumprimento das obrigações aplicáveis antes de reativar o recurso.
3. Se é um caso pontual (por exemplo, dependência de build sem licença OSI), acrescentar uma entrada em `exceptions` com `ecosystem` (`npm` ou `go`), `package` (nome npm ou caminho do módulo Go), `license` (exatamente a que o conferidor mostrou), `reason`, `date` e `expires` (no máximo seis meses, 183 dias; datas em AAAA-MM-DD, inválidas fazem o job falhar). A exceção vale só para aquele pacote e aquela licença, em qualquer versão, e **vence**: depois da data o job volta a falhar, para forçar a revisão. Exceção sem uso aparece como aviso e deve ser removida.

## Commits e pull requests

Commits seguem o [Conventional Commits 1.0.0-beta.4](https://www.conventionalcommits.org/pt-br/v1.0.0-beta.4/), **sempre em inglês**:

```
<type>[optional scope]: <description>

[optional body]

[optional footer]
```

- Tipos: `feat`, `fix`, `docs`, `chore`, `style`, `refactor`, `perf`, `test`, `ci`, `build` e `improvement`.
- Escopo: a parte do código afetada, por exemplo `feat(collector):`, `fix(tracker):`, `docs(adr):`.
- Descrição no imperativo, curta e em minúsculas: `feat(collector): add Stellar address normalization`.
- Quebra de compatibilidade: `BREAKING CHANGE: <descrição>` no corpo ou no rodapé, ou `!` antes dos dois-pontos.
- Feche a issue pelo rodapé: `closes #32`.

Pull requests:

- Um assunto por PR, com título no mesmo padrão dos commits.
- Descreva o que mudou e como testou.
- O CI precisa estar verde para o merge.

Documentação, ADRs e issues são escritos em português do Brasil.

## Licença

Ao contribuir, você concorda que sua contribuição será licenciada sob a [licença MIT](LICENSE) do projeto.
