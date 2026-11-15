# 0006. API de leitura do SCF com chaves de operador

- **Status:** Aceito
- **Data:** 15/11/2026
- **Issue:** [#48](https://github.com/romulospl/notify/issues/48)

## Contexto

Os operadores do SCF veem as métricas de todos os projetos nos painéis do portal. A issue #48 pede que eles gerem, listem e revoguem chaves de API e que outros sistemas (planilhas, Grafana, relatórios do SCF) leiam as mesmas métricas por uma API autenticada pela chave. A chave é de leitura: não escreve nada, e não pode ser confundida com a chave de ingestão (`ntf_`, aceita só pelo coletor).

O ADR 0003 faz do portal o dono do isolamento: o acesso a dados passa por funções que recebem um contexto tipado criado a partir da sessão. A API de leitura precisa seguir a mesma regra, com a chave no lugar da sessão.

## Decisão

- **Formato:** `nrk_<prefixo>_<segredo>`, mesma estrutura da chave de ingestão (prefixo base32 de 8 caracteres, segredo de 32 bytes em base64url). O marcador `nrk_` é diferente de `ntf_`: o coletor recusa pelo formato e a API de leitura também recusa `ntf_`. O banco guarda só o prefixo e o SHA-256; a chave completa existe apenas na resposta da criação.
- **Tabela `operator_api_key`** (migração 0006, privilégios na 0007): nome dado por quem cria, prefixo único, hash, criador, datas e último uso (no máximo uma escrita por dia). `notify_ingest` e `notify_collector` não têm permissão na tabela. Criar e revogar vão para o `audit_log` (`operator_api_key.created` e `.revoked`, sem tenant, com o prefixo e o nome, nunca a chave).
- **Escopo único:** toda chave lê as métricas globais dos projetos ativos, exatamente o que os painéis `/operator/web` e `/operator/api` mostram, e nada além disso (nenhum e-mail, ID do Umami ou dado de visitante). Projeto removido ou pendente não aparece, como nos painéis. Não há escopo por projeto nem de escrita.
- **A chave vale enquanto o operador que a criou for operador.** A autenticação confere `user.role = 'operator'` do criador a cada chamada, então rebaixar um operador desativa as chaves dele sem passo extra. Qualquer operador vê e revoga as chaves de todos (a equipe é uma só; o `audit_log` diz quem fez o quê). Máximo de 20 chaves ativas.
- **Onde roda:** no próprio portal, em rotas `/api/v1/*` (Route Handlers do Next.js). `authenticateReadKey` (em `src/lib/access.ts`, o único lugar que cria contextos) devolve um `OperatorContext`, e as rotas usam as mesmas funções de `src/lib/data/operator/` e `src/lib/umami/overview.ts` dos painéis. O lint de fronteiras continua valendo.
- **Rotas (v1), só `GET`:** `/api/v1/projects` (projetos ativos com contagens), `/api/v1/web?period=` (visualizações, visitantes, visitas, rejeições e tempo, com o período anterior) e `/api/v1/api?period=` (requisições, 4xx, 5xx, p50 e p95 em segundos). `period` aceita `24h`, `7d` (padrão), `30d`, `90d` e `12m`, em UTC; valor desconhecido responde 400 (diferente da tela, que volta ao padrão).
- **Sem paginação na v1.** As respostas têm uma linha por projeto ativo, e o número de projetos do ecossistema é pequeno (dezenas a poucas centenas). Se passar disso, a v2 ganha paginação por cursor; o prefixo `/v1` existe para isso.
- **Limite de requisições:** 60 por minuto por chave, janela fixa em memória, 429 com `Retry-After`. O `/web` faz uma chamada ao Umami por site, então o limite protege o Umami. Com mais de uma instância o limite vale por instância.
- **Respostas de erro** não dizem por que a chave foi recusada: 401 igual para ausente, malformada, desconhecida, errada, revogada ou de ex-operador. Todas levam `Cache-Control: no-store`.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Serviço separado (Go ou Node) para a API de leitura | Escala e implanta à parte | Segundo caminho de acesso a dados fora da camada única do ADR 0003; duplica as consultas dos painéis |
| Reaproveitar a chave de ingestão (`ntf_`) com um escopo | Uma só chave por conceito | Chave de ingestão é de tenant e fica em apps públicos; a de leitura é global e sensível. Misturar dá risco de uma abrir a porta da outra |
| Token de sessão do Better Auth (plugin de API key) | Pronto, com limite embutido | A telemetria e o cadastro do Better Auth são limitados de propósito (#60); o plugin traria um segundo modelo de chave diferente do da ingestão |
| Chave por projeto (escopo por tenant) | Menor alcance | Não é o que a issue pede (consumo global pelo SCF); o tenant já tem o painel dele |
| Redis ou tabela para o limite de requisições | Vale entre instâncias | Mais uma peça para poucas chaves; pode entrar depois sem mudar a API |

## Consequências

- Mais fácil: o SCF alimenta outros sistemas sem acessar o Umami nem o banco, e cada chave é revogável sem afetar as outras. Os números da API são os mesmos dos painéis.
- A chave de leitura é uma credencial de alto alcance: dá visão global. Fica fora de qualquer visão acessível ao gateway e ao coletor, e o hash nunca sai do banco.
- Revogação vale na próxima chamada (sem cache de chave).
- Sem proteção contra tentativas de adivinhar chaves além do formato e da entropia (40 bits de prefixo e 256 do segredo); limitar por IP fica para o Caddy.
- Testes: `apps/portal/test/read-api.test.ts` (ciclo da chave, autenticação, limite, respostas) e casos novos em `test/isolation.test.ts` (tenant não gera nem usa chave, chave de ingestão não lê a API, chave de leitura não vale no coletor, tenant removido some). O coletor tem o caso `nrk_` em `ingestkeyauth/auth_test.go`.
