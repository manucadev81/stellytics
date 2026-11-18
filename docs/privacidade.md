# Privacidade e GDPR

Este documento descreve o que a plataforma faz com dados pessoais, lendo o código da `main`, e lista o que ainda não existe. Ele **não é parecer jurídico**: os papéis (controlador e operador) e os pontos de atenção por jurisdição são uma **proposta para validar com assessoria jurídica**, e o que não foi conferido está marcado como tal. Atende à issue [#40](https://github.com/romulospl/stellytics/issues/40).

Convenções: "verificado" quer dizer que a afirmação foi conferida no código ou na configuração citados ao lado. "Não verificado" quer dizer que depende de comportamento de terceiros (Umami, provedor de SMTP, Hetzner) ou de algo que o repositório ainda não tem.

Seções verificadas diretamente no código: 3.1 (`tracker.ts`, `relay.go`), 3.3 (`auth.ts`, `schema.ts`) e 4 (`clientip.go`). As demais combinam código, specs e ADRs, como indicado em cada uma.

## 1. Resumo

- **Visitantes dos sites dos tenants (analytics web):** sem cookie, sem `localStorage`/`sessionStorage`, sem identificador persistente no navegador. O IP completo nunca é gravado: o gateway trunca antes de repassar ao Umami.
- **Chamadas de API dos tenants (analytics de API):** só contadores e histogramas por rota, método e status, a cada minuto. Traces, spans e logs são descartados.
- **Usuários do portal (e-mail, nome, senha com hash):** são dados pessoais e ficam no banco `stellytics`. O portal não grava o IP de quem entra.
- **Onde ficariam:** a IaC fixa `nbg1` (Hetzner, Alemanha) por padrão para servidor, volume e backup; **a produção ainda não foi aplicada** (`docs/andamento.md`).
- **Apagamento:** hoje **não há fluxo de apagamento** nem para visitantes nem para usuários. Para visitantes, a razão é que o IP completo e os identificadores persistentes não existem (seção 6), mas isso **não é o mesmo que "nenhum dado pessoal guardado"**: a seção 3 lista o que fica. Para usuários do portal, é uma lacuna (seção 7).

## 2. Papéis (proposta a validar)

| Dado | Quem decide finalidade e meios (proposto) | Papel da plataforma (proposto) |
|---|---|---|
| Eventos de visitantes dos sites do tenant e métricas de API do tenant | O projeto (tenant), que instala o script ou o OpenTelemetry | **Operador** (processor) |
| Conta no portal (e-mail, nome, senha, sessões) | A própria plataforma | **Controlador** |
| `audit_log` e registros de operação (remoção de projeto, chaves) | A plataforma | **Controlador** |

Consequências a tratar com o jurídico, **não resolvidas aqui**: quem é a entidade legal que opera a instância (o repositório é código aberto; quem hospeda assume o papel), contrato de tratamento (DPA) com cada tenant, texto de aviso de privacidade do portal e orientação aos tenants sobre o aviso que eles devem dar aos próprios visitantes. Os operadores do SCF veem dados de todos os projetos (seção 5); a relação entre o SCF e cada tenant também precisa de definição.

## 3. O que é coletado e onde fica

### 3.1 Analytics web

O script `packages/tracker` envia um page view por carregamento (e por troca de rota em SPA) com estes campos (`packages/tracker/src/tracker.ts`): `website` (ID do site), `hostname`, `url` (caminho mais só os parâmetros `utm_*`; o resto da query string é descartado), `referrer` (origem e caminho, sem query nem fragmento; vazio se for a mesma página), `title`, `language` (`navigator.language`) e `screen` (largura x altura). Junto vai a prova de execução de JS, que o gateway não repassa.

O gateway (`services/ingest-web/internal/relay/relay.go`) remonta o payload só com esses campos e repassa ao Umami com o `User-Agent` original e o IP **já truncado** num header. Nunca repassa `ip`, `userAgent`, `id` nem `timestamp` vindos do cliente.

O Umami (v3.4.1, sem patch, `docs/umami.md`) grava os eventos no banco `umami`. O que o Umami cria a partir disso (sessão, navegador, sistema, dispositivo, país/região/cidade pela geolocalização do IP truncado) é **comportamento do Umami e não foi lido aqui**. Foi verificado apenas que o país da sessão sai do IP truncado e que o IP completo não aparece em nenhuma coluna de texto dos bancos `umami` e `stellytics` (`services/ingest-web/e2e/privacy_test.go`, roda no job `compose` do CI).

Dois pontos de atenção: o `url` e o `title` são dados que a página do tenant define. Um caminho como `/perfil/maria@exemplo.com` ou um título com nome próprio seriam gravados. O script já descarta a query string (exceto `utm_*`), mas o tenant deve evitar dado pessoal em caminhos e títulos.

### 3.2 Analytics de API

O coletor (`services/collector`) autentica pela chave de ingestão, grava o tenant da chave e ignora o que o cliente disser sobre isso. O exporter `stellyticspg` transforma spans de servidor HTTP e a métrica de duração em pontos por minuto na tabela `api_metric_1m`: tenant, serviço, rota normalizada, método, status, contagem, soma de durações e histograma (`docs/specs/e5-pipeline-de-api.md`, seção 4). Traces, spans e logs são descartados e não são gravados. Não há IP nem identificador de usuário final nas colunas.

Ponto de atenção: a rota é texto. Segmentos numéricos, UUIDs, endereços Stellar e hashes hex viram marcadores (`{id}`, `{uuid}`, `{address}`, `{hash}`), mas outro segmento que carregue dado pessoal (por exemplo `/users/maria`) seria gravado como veio, e um `http.route` enviado pelo cliente vale como veio. Mitigação parcial: teto de 500 rotas por serviço e dia, depois `{other}`.

### 3.3 Contas do portal

Tabelas `user`, `account`, `session` e `verification` (`apps/portal/src/lib/db/schema.ts`):

- `user`: nome, e-mail, papel e datas. `account`: senha com hash (Better Auth). `tenant_member`: vínculo do usuário com o projeto.
- `session`: token, expiração e **`user_agent`**. O `ip_address` é sempre nulo, porque o hook `databaseHooks.session.create.before` em `apps/portal/src/lib/auth.ts` o zera. Ou seja, o User-Agent de quem entra no portal **é gravado**; o IP não.
- `verification`: tokens de confirmação de e-mail e de redefinição de senha.
- O Better Auth usa um cookie de sessão no portal (necessário para o login). Sessão de 7 dias, renovada a cada dia de uso. Isso vale só para o portal, não para os sites dos tenants.
- O limite de tentativas por IP (`rateLimit` em `auth.ts`) é em memória, por instância. Se o Better Auth ou o Next.js registram o IP em log em algum ponto **não foi verificado**.

### 3.4 Registro de auditoria e chaves

`audit_log` recebe ação, quem fez (`actor_user_id`), tenant, data e `details` (`tenant.created`, `tenant.removed`, `tenant.reactivated`, `operator.created`, `ingest_key.created`, `ingest_key.revoked`, entre outras). O código e o comentário do esquema dizem que não leva chave, senha nem IP. O motivo da remoção de um projeto é texto livre digitado pelo operador e vai para `details` e também para `tenant.status_reason`; `tenant.status_changed_by` guarda o operador (chave estrangeira para `user` com `onDelete: set null`), ou seja, apagar o operador zera esse campo, mas não o `actor_user_id` do `audit_log`. A tabela só aceita inserção (gatilhos em `apps/portal/drizzle/0002_ingest_views.sql` contra UPDATE, DELETE e TRUNCATE) e não tem chave estrangeira, de propósito: sobrevive à exclusão do usuário ou do tenant. Isso entra em conflito com um futuro apagamento de conta (seção 7): o `actor_user_id` fica, e corrigir ou apagar uma linha à mão exigiria desabilitar o gatilho.

A chave de ingestão completa nunca é gravada; só o prefixo e o SHA-256 (`ingest_key`), e `last_used_on` guarda só a data.

### 3.5 E-mail

O portal envia por SMTP (`apps/portal/src/lib/mail.ts`, variável `SMTP_URL`) só duas mensagens, em texto puro: confirmação de e-mail e redefinição de senha. Em falha, o log leva só o tipo do erro, sem destinatário nem corpo. Qual provedor de SMTP será usado em produção **não está definido no repositório** (`infra/ansible/secrets.example.yml` só traz um exemplo). O provedor será um suboperador: precisa estar na UE ou ter garantias de transferência, e isso **não foi verificado**. Não há e-mail de marketing.

### 3.6 Logs e memória

- **Gateway:** nenhum log leva IP, `X-Forwarded-For` ou `User-Agent`; um panic é registrado com `path` e `fmt.Sprint(v)` (`handler.go`, `recoverPanic`), onde `v` é um valor arbitrário do panic: não leva IP por desenho, mas o conteúdo não é controlado. O nível do log é o padrão do `slog` (Info), então os logs `Debug` de "evento descartado" ficam desligados. Nenhuma resposta grava cookie. O limite de requisições por IP e a contagem de fingerprints (hash de User-Agent, `Accept-Language`, idioma e tela) usam o IP **truncado**, só em memória e por janela curta (`internal/limit`, `internal/fingerprint`).
- **Nginx:** o vhost não configura access log e grava apenas erros críticos em arquivo dedicado (`infra/ansible/roles/stellytics/templates/nginx.conf.j2`). Logs globais do host podem seguir a configuração padrão do sistema.
- **Coletor:** quando a consulta à `ingest_key_active` falha, o log leva o prefixo da chave (`ingestkeyauth/auth.go`), que é a parte pública e não é dado pessoal; a chave completa não. Uma leitura de todos os logs do coletor **não foi feita**.
- **Portal:** o logger do Better Auth está em `warn` (`auth.ts`), e o envio de e-mail loga só o tipo do erro.
- **Contêineres:** em produção usam `json-file` com 20 MB por arquivo e 5 arquivos (`x-logging` em `infra/compose/prod.yaml`). A rotação é por tamanho, não por tempo, então não há prazo fixo de retenção dos logs. O conteúdo dos logs do Umami e do Postgres **não foi verificado**.

## 4. Truncamento de IP

`services/ingest-web/internal/clientip/clientip.go`, função `Truncate`: zera o último octeto do IPv4 (`203.0.113.77` vira `203.0.113.0`) e tudo depois do `/48` do IPv6; IPv4 mapeado em IPv6 é tratado como IPv4. Testado em `clientip_test.go`. O IP completo existe só na memória do request. O `X-Forwarded-For` só vale vindo de `TRUSTED_PROXIES` (o gateway Docker do host Nginx em produção). Limite honesto: um IPv4 truncado ainda identifica uma rede de até 256 endereços, e o `User-Agent` completo continua indo ao Umami. Se esse conjunto é "dado pessoal" depende da avaliação jurídica e do que o Umami grava (não verificado).

## 5. Quem acessa

- Cada tenant vê só os próprios dados; o tenant vem sempre da sessão (ADR 0003, suíte `apps/portal/test/isolation.test.ts`).
- O operador do SCF tem leitura global (painéis `/operator`, `/operator/web`, `/operator/api`) e hoje só escreve para remover e reativar projeto. Operadores nascem só por comando de administração, que grava no `audit_log`.
- O Umami fica só na rede interna: sem porta publicada e sem rota no Nginx (`docs/umami.md`, #62). Gravação de sessão e heatmaps estão desligados, e há teste (`TestRecorderRecordsNothing`).
- Quem administra a VM e o banco por SSH também acessa tudo. Não há controle de acesso documentado além do túnel de administração (`docs/deploy.md`); processo de acesso e registro **não verificados**.

## 6. Residência dos dados e retenção

**Onde ficam** (ADR 0004): a IaC (`infra/opentofu/variables.tf`) fixa por padrão a Hetzner Cloud em Nuremberg (`nbg1`), Alemanha; **a produção ainda não foi aplicada**, então hoje nenhum dado de produção está lá. Pela IaC, a VM, o volume com o Postgres e o Object Storage dos backups ficam no mesmo local; o local é uma variável (`nbg1`, `fsn1` ou `hel1`, todos na UE). Os backups do WAL-G saem da VM **cifrados no cliente** (chave fora da VM). O monitoramento externo (ADR 0005) roda numa VM separada em `fsn1` (Falkenstein, Alemanha) e, pelo ADR, serve para checar disponibilidade; **não verifiquei** se algum dado de analytics chega a ele. O Hetzner é um suboperador; contrato e localização do suporte dele **não verificados**. A UE como residência é decisão de implantação: a instância que outra equipe subir pode escolher outro local.

**Quanto tempo ficam:**

| Dado | Retenção | Fonte |
|---|---|---|
| `api_metric_1m` | 35 dias, por `DROP` de partição mensal | migração `0005_api_metric_rollup.sql`, spec do E5 |
| `api_metric_1h`, `api_metric_1d` | 25 meses | idem |
| Eventos web no Umami | **Não encontrei retenção configurada**; sem ela, ficam indefinidamente | nenhum job nem variável no `compose`/Ansible; busca pontual, não exaustiva |
| Conta, sessões, tenants, sites, chaves | Até alguém apagar à mão; **sem fluxo** | seção 7 |
| `audit_log` | Para sempre (só inserção) | esquema |
| Backups | Os 7 backups base mais recentes mais WAL contínuo (ADR 0004). Dados apagados continuam nos backups até saírem da janela | ADR 0004 |

O serviço de consolidação (`services/rollup`) e o coletor estão no `infra/compose/prod.yaml` desde 16/11/2026 (ADR 0012). A retenção de API descrita acima está implementada e testada, mas nenhuma produção real foi aplicada ainda. O vhost Nginx não configura access log e registra apenas erros críticos; assim, os cabeçalhos, inclusive a chave de ingestão (`Authorization`), não são escritos pelo proxy.

## 7. Direito ao apagamento e outros direitos: lacunas

A issue pede a justificativa para não haver fluxo de apagamento por "nenhum dado pessoal guardado". Depois de ler o código, a frase só vale em parte, e o documento não promete mais do que isso.

**Visitantes dos sites (analytics web):**

- O navegador não guarda identificador (sem cookie ou storage), o gateway não repassa `id` do cliente e o IP completo não é gravado. Mas o Umami deriva um identificador de sessão (`session_id`) a partir de IP truncado, `User-Agent` e um sal; essa derivação **não foi analisada** aqui. Por isso, "não dá para localizar os dados de uma pessoa" é uma hipótese, não uma justificativa firme para dispensar o fluxo de apagamento de visitantes. Está no checklist da seção 9.
- Também não vale dizer "nenhum dado pessoal": o Umami recebe IP truncado e `User-Agent` e deriva sessão, navegador, sistema, dispositivo e localização; o que ele **persiste** não foi verificado. E o caminho e o título que o tenant manda podem conter dado pessoal. Se isso é dado pessoal sob o GDPR ou é anonimização efetiva é **questão jurídica em aberto**, não decidida aqui.
- Arquivar um site (`site.archived_at`) para a ingestão e tira o site dos painéis, mas **nenhum fluxo apaga os dados já coletados** no Umami. Apagar à mão no banco `umami` é possível a quem administra, mas não há processo.

**Usuários do portal (controlador: a plataforma):**

- **Não existe exclusão da própria conta** (lacuna apontada em `docs/andamento.md`, item 9, sem issue) nem exportação dos dados. Também não existe convite de membros; hoje só quem cria o projeto tem acesso.
- **Remover um projeto é desativação** (#20, `removeTenant`): muda o status, grava quem (`status_changed_by`), o motivo em texto livre (`status_reason`) e um `audit_log`, bloqueia a ingestão e some dos painéis. Os dados continuam no banco. O spec do E2 deixa "apagar os dados de um tenant removido" como ação separada e ainda inexistente.
- Um pedido de correção de dado no `audit_log` só poderia ser atendido desabilitando o gatilho da migração `0002`, que bloqueia UPDATE, DELETE e TRUNCATE.
- No esquema, `session`, `account` e `tenant_member` apagam em cascata quando o `user` é apagado, e `audit_log` não tem chave estrangeira, então manteria o `actor_user_id`. Nenhuma tela ou comando apaga usuário hoje.
- Retificação (mudar nome ou e-mail), portabilidade, limitação e oposição: **não há tela nem processo**. O pedido hoje seria atendido à mão por quem administra o banco.

**Próximos passos sugeridos** (nenhum tem issue ainda, a abrir): exclusão da própria conta com tratamento do `audit_log` (por exemplo, trocar o `actor_user_id` por um marcador); apagar dados de um tenant removido (Umami e `api_metric_*`); retenção dos eventos web no Umami; processo documentado de pedidos de titulares; DPA e aviso de privacidade.

## 8. Jurisdições: lista provisória (proposta)

A issue pede a lista de jurisdições onde a solução não seria conforme sem opt-in. **Este critério é atendido só parcialmente:** não há conclusão jurídica no repositório, e este documento não a inventa. Abaixo está uma lista **provisória, para validar com assessoria jurídica**, que separa onde há regime próprio de consentimento para acesso ao dispositivo (que é o ponto de partida da dúvida) do que não foi analisado.

O que o código faz, verificado: o script não escreve nem lê cookie, `localStorage` ou `sessionStorage` e não guarda identificador no navegador (`packages/tracker/src/tracker.ts`). Ele lê `navigator.language`, `screen`, `document.title` e `document.referrer` e os envia.

**A rever primeiro (regime de consentimento para acesso ao dispositivo, conclusão em aberto):**

| Jurisdição | Ponto a revisar |
|---|---|
| UE/EEE (GDPR e diretiva ePrivacy, art. 5(3)) | Se ler `language` e `screen` e enviar IP truncado e `User-Agent` ao Umami exige consentimento ou base legal. **Em aberto** |
| Alemanha (TDDDG, antes TTDSG) e França (orientação da CNIL sobre medição de audiência) | Orientações próprias, com condições para dispensar consentimento. As condições (como limite de retenção) **não foram conferidas aqui**; a falta de retenção nos eventos do Umami (seção 6) é provável ponto de conflito |
| Reino Unido (UK GDPR e PECR) | Regime análogo, com orientações do ICO; **não verificado** |

**Não analisadas** (podem ter regras próprias de privacidade ou de consentimento): Brasil (LGPD), Califórnia (CCPA/CPRA), Suíça e demais jurisdições. Aplicação e enquadramento, inclusive o fato de o código não enviar dados a terceiros de publicidade (verificado só no gateway e no portal), não substituem essa análise.

Não há base no repositório para concluir, para nenhum dos lados, se o consentimento é ou não exigido. Cada tenant que instala o script decide se mostra aviso ou banner; este projeto não oferece opt-in.

Não há transferência para fora da UE por parte do código: o gateway só fala com o Umami interno. Tenants fora da UE enviam dados de visitantes para a UE; isso, e qualquer transferência por suboperadores (SMTP, backup), precisa de análise.

## 9. Checklist do que verificar com assessoria jurídica

1. Entidade legal operadora, papéis da seção 2 e DPA com tenants (e com o SCF).
2. Qualificação de IP truncado (/24, /48) mais User-Agent como dado pessoal ou não.
3. Necessidade de consentimento por jurisdição (seção 8).
4. Retenção dos eventos do Umami e conclusão sobre `audit_log` permanente.
5. Suboperadores: Hetzner (VM e Object Storage), provedor de SMTP e, se houver, o do monitoramento.
6. Aviso de privacidade do portal e texto-modelo para os tenants.
7. Processo de pedidos de titulares enquanto não houver fluxo automático.
8. Como o Umami deriva e persiste o `session_id` e os demais campos da sessão (IP truncado, `User-Agent`, sal), e se isso permite ou dispensa um fluxo de apagamento de visitantes.
9. Gatilho de `audit_log` e pedidos de correção ou apagamento (seção 3.4).

## 10. Arquivos conferidos

`services/ingest-web/internal/clientip/clientip.go`, `services/ingest-web/internal/relay/relay.go`, `services/ingest-web/internal/fingerprint/fingerprint.go`, `services/ingest-web/handler.go`, `services/ingest-web/main.go` (logs), `services/ingest-web/e2e/privacy_test.go` (lido o cabeçalho e o teste de IP), `packages/tracker/src/tracker.ts`, `apps/portal/src/lib/auth.ts`, `apps/portal/src/lib/mail.ts`, `apps/portal/src/lib/db/schema.ts`, `apps/portal/src/lib/data/operator/tenants.ts`, `infra/ansible/roles/stellytics/templates/nginx.conf.j2`, `infra/compose/prod.yaml` (lista de serviços), `docs/adr/0004-hospedagem-e-backup.md`, `docs/umami.md`, `docs/specs/e5-pipeline-de-api.md` e `docs/specs/e2-tenants-e-controle-de-acesso.md` (trechos sobre retenção e remoção).

**Não lidos nem testados:** o código do Umami (campos gravados na sessão), as migrações `0002` e `0005` em SQL (só os resumos do `CLAUDE.md` e dos specs), o código completo do coletor e do exporter, logs reais de execução, a configuração real do SMTP e do monitoramento.
