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
- **Limite de requisições, em memória, por instância:**
  - por chave: 60 por minuto depois de autenticada (janela fixa), 429 com `Retry-After`. O `/web` faz uma chamada ao Umami por site, então o limite protege o Umami;
  - por IP truncado, **antes** da consulta ao banco: 120 consultas de chave por minuto (IPv4 /24, IPv6 /48). Só conta header com o formato `nrk_<8>_<43>`; o resto leva 401 sem tocar no banco nem no limite. O IP vem da última entrada do `X-Forwarded-For`, a que o Caddy escreve (ele não tem `trusted_proxies`, então descarta a que o cliente mandou, e o portal não publica porta em `prod.yaml`). Fora do Caddy, sem o cabeçalho, todos caem num balde só.
- **Respostas de erro** não dizem por que a chave foi recusada: 401 com o mesmo status, corpo e cabeçalhos para ausente, malformada, desconhecida, errada, revogada ou de ex-operador (testado). O esquema `Bearer` não diferencia maiúsculas (RFC 7235). Todas levam `Cache-Control: no-store`.
- **Sem CORS, de propósito:** a API é servidor a servidor. O portal não responde ao preflight nem manda `Access-Control-Allow-Origin`, então um navegador de outra origem não lê as respostas, e a chave nunca deve ir para código de navegador.
- **Último uso** (`last_used_on`) é gravado em melhor esforço: se a escrita falhar, a chamada segue com 200 e o log leva só o nome do erro.

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
- Revogação vale na próxima chamada (sem cache de chave).
- A chave de leitura é uma credencial de alto alcance: dá visão global. Fica fora de qualquer visão acessível ao gateway e ao coletor, e o hash nunca sai do banco.

### Riscos e pendências

- **Pendência: o Caddy não limita por IP.** O `Caddyfile` e a imagem do Caddy de `prod.yaml` não têm módulo de limite de requisições, e esta decisão não mudou nenhum dos dois. O Caddy faz só duas coisas aqui: encaminhar `/api/v1` para o portal (a rota do host do portal cobre tudo) e escrever o IP real no `X-Forwarded-For`. O limite por IP é do portal, em memória e por instância: ele se perde ao reiniciar e não soma entre instâncias. Um ataque distribuído (muitos IPs) não é contido por ele. Se isso virar problema, o passo seguinte é um limite no proxy (imagem do Caddy com módulo de rate limit, ou firewall) ou um limite compartilhado; não foi feito.
- **O que a recusa revela.** O caminho de prefixo inexistente, chave revogada ou de ex-operador responde sem calcular o SHA-256, então um atacante com medida de tempo fina consegue distinguir "prefixo ativo" de "prefixo inexistente ou inativo". Isso revela só se o prefixo existe e está ativo, e o prefixo já é público (aparece na tela `/operator/keys` para todo operador). O segredo é comparado em tempo constante.
- **Escopo único e global:** vazar uma chave expõe as métricas de todos os projetos ativos até a revogação. Mitigação: revogação imediata, 20 chaves no máximo, auditoria de criação e revogação. Não há auditoria de leitura (só `last_used_on`, diário).
- O limite por chave também é por instância.
- O `/web` depende do Umami: com o Umami fora, o projeto vem com `totals` nulo e `unavailable` maior que zero, em vez de erro.

### Testes

- `apps/portal/test/read-api.test.ts`: formato, ciclo da chave, hash e audit_log, 401 idêntico em todos os casos, Bearer sem diferenciar caixa, falha na escrita do último uso, limite por chave e por IP (sem consulta ao banco depois do limite), respostas e período inválido.
- `apps/portal/test/isolation.test.ts`: tenant e usuário sem projeto não geram nem revogam chave, chave de ingestão não lê a API, chave de leitura não existe para o coletor e os papéis `notify_ingest` e `notify_collector` não leem a tabela, tenant removido some.
- `services/collector/components/ingestkeyauth/auth_test.go`: o coletor recusa `nrk_`.
