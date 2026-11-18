# Guia de onboarding de tenant

Para quem vai medir um projeto do ecossistema Stellar na plataforma: do cadastro até ler os painéis, cobrindo analytics **web** (script de rastreamento) e de **API** (OpenTelemetry). Escrito para a issue [#43](https://github.com/romulospl/stellytics/issues/43) (critério do piloto pendente).

> **Estado da validação.** Os trechos deste guia foram conferidos contra os exemplos do repositório e contra o código das telas (ver o PR #108). A validação com um **piloto real** (critério de aceite 2 da #43) **ainda não aconteceu**: depende dos pilotos das issues #45 e #46. Até lá, trate o guia como não validado por um projeto externo.

Convenções: `<gateway>` é o host de ingestão web e `<coletor>` o endereço do coletor de API que o operador da instância informa. No Compose local ele fica em `127.0.0.1:4318` (HTTP) e `4317` (gRPC); em produção é `https://<api_host>`, na porta 443, para OTLP/HTTP e gRPC (ADR 0012). Nenhuma produção real foi aplicada ainda (`docs/andamento.md`); confirme o endereço com quem opera a instância antes de configurar.

## 1. Visão geral

Um **projeto** (tenant) agrupa **sites** (analytics web) e **serviços de API** (analytics de API). Você só vê os dados do seu projeto; operadores do SCF veem todos os projetos.

| Passo | Onde | O que sai dele |
|---|---|---|
| 1. Cadastro e confirmação de e-mail | `/sign-up` | conta confirmada |
| 2. Criar o projeto | `/onboarding` | projeto, com você como `owner` |
| 3a. Registrar um site | `/sites` | ID do site para o script |
| 3b. Registrar um serviço de API | `/services` | chave de ingestão `ntf_...` |
| 4a. Instalar o script | no seu site | page views no painel |
| 4b. Configurar dual-export OTel | na sua API | métricas no painel |
| 5. Ler os painéis | `/sites/[siteId]`, `/services/[serviceId]` | |

Você pode usar só a parte web, só a de API, ou as duas.

## 2. Cadastro

1. Abra `/sign-up` e informe nome, e-mail e senha (mínimo de 12 caracteres, com confirmação).
2. O portal envia um e-mail com o link de confirmação, válido por 24 horas. **Sem confirmar, você não entra nem cria projeto.** Com o link válido, você já entra logado e segue para o projeto. Se o link vencer ou for inválido, vá a `/sign-in` com e-mail e senha: o portal manda um novo (há limite por IP).
3. Em `/onboarding`, dê um nome ao projeto (por exemplo, o nome do seu produto). Isso cria o projeto e a sua função de `owner`.
4. O `/dashboard` mostra a contagem de sites e de serviços, com links para `/sites` e `/services`.

Esqueceu a senha? `/forgot-password` envia um link de uso único, válido por 30 minutos, que encerra as outras sessões. A sessão do portal dura 7 dias de inatividade (renova a cada dia de uso).

O que **não existe**: convite de outros membros para o projeto e exclusão da própria conta. Um projeto tem a conta que o criou. Quem precisar remover um projeto fala com os operadores do SCF (a remoção é feita por eles, com motivo registrado, e pode ser revertida).

## 3. Analytics web

### 3.1 Registrar o site

Em `/sites`, informe **Name** e **Domain** (por exemplo `docs.example.org`) e adicione. A tabela passa a mostrar o **Website ID**, um UUID. É esse valor que vai no atributo `data-site` do script. O nome do site leva ao painel dele.

O portal **não gera o trecho de instalação**: monte a tag como abaixo, trocando `<gateway>` e `<website-id>`.

### 3.2 Instalar o script

O script fica no gateway, com a versão no caminho e um hash SRI. Os hashes de todas as versões estão em [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json). A versão mais nova é a **v1.1.0**.

**HTML estático e SPA.** No `<head>` de cada página (numa SPA, só no `index.html`):

```html
<script
  defer
  src="https://<gateway>/s/v1.1.0/n.js"
  integrity="sha384-oJV5jTaVFhvi5Y8R/QGZrzfVps1szxTK74tBPsk7LIsuUoUzzFetJ//8xd0NUqs0"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

Numa SPA não precisa de mais nada: o script conta as mudanças de rota feitas por `history.pushState`, `history.replaceState` e pelo voltar do navegador. Roteadores como React Router, Vue Router e o do SvelteKit usam essas APIs. Exemplos: [`examples/static-html`](../examples/static-html), [`examples/spa`](../examples/spa).

**Next.js (App Router, com SSR).** No `app/layout.tsx`, dentro do `<head>`:

```tsx
<head>
  <script
    defer
    src="https://<gateway>/s/v1.1.0/n.js"
    integrity="sha384-oJV5jTaVFhvi5Y8R/QGZrzfVps1szxTK74tBPsk7LIsuUoUzzFetJ//8xd0NUqs0"
    crossOrigin="anonymous"
    data-site="<website-id>"
  />
</head>
```

A tag vai no HTML que o servidor gera, e a navegação pelo `<Link>` conta como page view. **Não use o `next/script` com `strategy="lazyOnload"`:** ele carrega o script tarde, e o primeiro page view pode se perder. Exemplo: [`examples/nextjs-ssr`](../examples/nextjs-ssr).

Os trechos acima são os mesmos de [`examples/README.md`](../examples/README.md), que os exemplos reais e o teste ponta a ponta (`examples/e2e`) exercitam.

### 3.3 Versão imutável e SRI

- Cada versão publicada (`/s/vX.Y.Z/n.js`) **nunca muda**: o arquivo é servido com cache imutável de um ano e o hash vem de `sri.json`. Se o conteúdo mudasse, o navegador recusaria o script pelo `integrity`.
- O script **nunca se atualiza sozinho**. Para trocar de versão, mude o caminho e o `integrity` no seu HTML.
- Use sempre a versão **v1.1.0 ou mais nova** (a **v1.2.0** acrescenta eventos customizados e a **v1.3.0**, `window.notify.wallets`; **a v1.4.0 renomeia o objeto global para `window.stellytics`**). A v1.0.0 não faz a prova de execução de JS, e o gateway **descarta os eventos dela**.
- `crossorigin="anonymous"` é obrigatório junto com `integrity`; o gateway responde com `Access-Control-Allow-Origin: *` para isso.

### 3.4 Requisitos e o que o script faz

- **HTTPS:** a prova de execução de JS usa `crypto.subtle`, que o navegador só oferece em HTTPS ou `localhost`. Numa página HTTP comum o script não envia eventos.
- O script pede um desafio em `GET /c`, resolve a prova e envia um page view em `POST /e` para a origem de onde foi carregado. Os eventos de quem não passa na prova são descartados em silêncio (`204`).
- Um page view carrega: ID do site, hostname, caminho (mais só os parâmetros `utm_*`), referenciador sem query nem fragmento, título, idioma e tamanho de tela.
- **Não ponha dado pessoal em caminhos ou títulos** (por exemplo `/perfil/maria@exemplo.com`): eles seriam gravados.
- Site arquivado ou projeto removido: o gateway responde como se tivesse aceitado e descarta o evento. Se os números não aparecem, confira o ID do site antes de qualquer outra coisa.

### 3.4.1 Eventos customizados (v1.2.0)

Com a v1.2.0, `window.stellytics.event(nome, propriedades)` registra uma ação (clique, conexão de carteira etc.) na página atual:

```js
window.stellytics?.event("signup_clicked", { plan: "pro", seats: 3 });
```

- Nome: 1 a 50 caracteres entre letras, números e `_ . : -`, começando por letra ou número. Propriedades: até 10, com chave de até 40 caracteres e valor texto (até 100 bytes), número ou verdadeiro/falso. O que fugir disso é ignorado.
- **Não envie dado pessoal nem de carteira:** endereço, chave pública, hash de transação, valor transferido, e-mail. O gateway limita só tipo e tamanho; o conteúdo é responsabilidade do projeto.
- `window.stellytics` só existe depois que o script carrega, e **não há fila**: uma chamada antes disso se perde. Use sempre `window.stellytics?.event(...)`.
- **Eventos do Stellar Wallets Kit (v1.4.0, opt-in, não é automático):** o script **não** captura nada do kit sozinho. Ele só alcança o kit se **você** o entregar, com uma chamada, depois de iniciar o kit (ver [ADR 0010](adr/0010-adaptador-opt-in-do-wallets-kit.md)):

```js
window.stellytics?.wallets(StellarWalletsKit, KitEventType);
```

  - Chame **uma vez** (chamar de novo com o mesmo kit não duplica a assinatura). Ela devolve uma função que cancela a assinatura. Se o kit ou o enum forem inválidos, nada acontece e a página não quebra.
  - Só assina `WALLET_SELECTED` (envia `wallet_selected` com `{ wallet: "freighter" }`, o ID do módulo) e `DISCONNECT` (envia `wallet_disconnected`, sem dados). **Nunca** assina `STATE_UPDATED` e nunca envia endereço, chave pública, `networkPassphrase`, hash ou valor. Um ID que não seja um nome simples de módulo (até 32 caracteres) é ignorado.
  - Como `window.stellytics` não tem fila, chame depois de o script carregar (`window.stellytics?.wallets(...)`; se o script puder demorar, repita a chamada no `load` da janela, ela é idempotente).
  - Se preferir assinar à mão e usar `stellytics.event`, o handler recebe `{ eventType, payload }`: o id está em `e.payload.id`, **nunca** em `e.id`, e `STATE_UPDATED` continua proibido (traz o endereço):

```js
StellarWalletsKit.on(KitEventType.WALLET_SELECTED, (e) => {
  const id = e?.payload?.id;
  if (id) window.stellytics?.event("wallet_selected", { wallet: id });
});
```
- O painel web ainda não lista os eventos customizados; eles ficam no Umami.

### 3.5 Ad blockers

O script e os endpoints usam `/s/`, `/e` e `/c`, sem os termos que as listas padrão bloqueiam, e um check diário confere as listas do uBlock Origin. Mesmo assim, **use um host neutro** para o gateway (por exemplo `e.<seu-domínio>` ou `gw.<seu-domínio>`), sem `analytics`, `track`, `collect`, `umami`, `stats`, `metrics` ou `telemetry` no nome. Resultado, listas testadas, limites (AdGuard e filtros de DNS não foram testados) e roteiro manual em [`packages/tracker/docs/ad-blockers.md`](../packages/tracker/docs/ad-blockers.md). Alguma parte dos visitantes ainda terá bloqueio; os números são uma medição, não um censo.

## 4. Analytics de API

### 4.1 Registrar o serviço e pegar a chave

1. Em `/services`, informe **Service name** (por exemplo `horizon-proxy`) e crie. O portal cria o serviço **e a primeira chave de ingestão**.
2. A chave aparece **uma única vez**, num aviso "Copy your ingest key now. It won't be shown again". Copie e guarde num gerenciador de segredos. Ela tem o formato `ntf_<8 caracteres>_<segredo>`. O portal guarda só o hash; se você perder a chave, crie outra.
3. Em `/services/[serviceId]/keys` (aba **Ingest keys**) você vê o prefixo, a data de criação e o status de cada chave (a coluna **Last used** mostra o dia, em UTC, da última autenticação da chave no coletor; "Never" quer dizer que ela ainda não foi usada), cria novas e revoga as antigas. Cada serviço pode ter até **5 chaves ativas**. Para trocar a chave sem perder dados: crie a nova, atualize a sua API e só então revogue a antiga.

**Uma chave por serviço.** A chave define o projeto e o serviço de cada métrica. O `service.name` e qualquer atributo de tenant que a sua API mandar são **ignorados**: o coletor apaga os atributos `stellytics.*` que vierem do cliente.

### 4.2 O que o coletor precisa receber

- **Endpoint:** OTLP/HTTP (`/v1/metrics`, `/v1/traces`) ou OTLP/gRPC, no `<coletor>`.
- **Header:** `Authorization: Bearer ntf_...`. Sem chave válida (ou com chave revogada, ou de projeto removido), a resposta é `401` (HTTP) ou `Unauthenticated` (gRPC).
- **Sinal:** o histograma `http.server.request.duration` (em segundos, o nome atual das convenções semânticas do OpenTelemetry; a antiga `http.server.duration`, em `ms`, também vale). Alternativa: spans de servidor HTTP. Se o serviço manda as duas coisas, o stellytics usa a métrica e ignora os spans, para não contar em dobro. Prefira a métrica: traces com amostragem (sampling) contam menos requisições do que as reais.
- **Atributos lidos:** `http.request.method`, `http.response.status_code` e `http.route`. Sem `http.route`, o coletor usa o caminho (`url.path`) e o normaliza.
- **Temporalidade:** prefira **delta**. Cumulativo também funciona (o coletor converte), mas um reinício do coletor pode perder um intervalo.

**Rotas normalizadas.** Um `http.route` enviado por você vale como veio (ex.: `/accounts/{id}`). Sem ele, os segmentos do caminho viram `{uuid}`, `{address}` (endereços Stellar G, C ou M), `{hash}` (hex de 16 caracteres ou mais) ou `{id}` (só dígitos), e query, fragmento e barra final somem. Há um teto de 500 rotas por serviço e dia; o que passar disso entra como `{other}`. Mande `http.route` sempre que seu framework o conhece.

**O que o coletor descarta.** Traces, spans e logs **nunca são gravados**: de cada requisição só entram rota, método, status, contagem e a distribuição de duração, agregados por minuto. Mesmo assim, não ponha dado pessoal em atributos; o filtro mais seguro é o do seu lado (um processor `filter` no seu Collector).

### 4.3 Dual-export pelo SDK

Sua API continua mandando a telemetria para o APM atual e manda a mesma métrica ao stellytics. Cada destino é um `PeriodicExportingMetricReader` no mesmo `MeterProvider` (código completo em [`examples/api-backend/server.mjs`](../examples/api-backend/server.mjs)):

```js
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-proto";
import { AggregationTemporality, MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";

const reader = (url, headers = {}) =>
  new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter({ url, headers, temporalityPreference: AggregationTemporality.DELTA }),
  });

const provider = new MeterProvider({
  readers: [
    reader("https://apm.example.com/v1/metrics"), // backend próprio
    reader("https://<coletor>/v1/metrics", { Authorization: `Bearer ${process.env.STELLYTICS_INGEST_KEY}` }),
  ],
});
```

As variáveis padrão `OTEL_EXPORTER_OTLP_*` configuram um destino só; o segundo precisa ser montado no código (ou use o Collector). Em outras linguagens a ideia é igual: dois exporters OTLP, um com o header `Authorization`.

### 4.4 Dual-export pelo OTel Collector

Se a sua API já manda para um OTel Collector, basta acrescentar um exporter (arquivo completo em [`examples/api-backend/otelcol.yaml`](../examples/api-backend/otelcol.yaml)):

```yaml
exporters:
  otlp_http/stellytics:
    endpoint: ${env:STELLYTICS_OTLP_ENDPOINT}
    headers:
      Authorization: Bearer ${env:STELLYTICS_INGEST_KEY}

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug, otlp_http/stellytics] # troque debug pelo exporter do seu APM
```

Funciona com o Collector core (`otel/opentelemetry-collector`) a partir da v0.164.0; em versões mais antigas, o nome do exporter é `otlphttp`. Com o contrib, dá para usar uma pipeline separada com um processor `filter` antes do `otlp_http/stellytics`, mandando só o necessário. Mais detalhes, incluindo a execução local com Compose, em [`examples/api-backend/README.md`](../examples/api-backend/README.md); o contrato completo está em [`docs/specs/e5-pipeline-de-api.md`](specs/e5-pipeline-de-api.md).

### 4.5 Conferir que chegou

A coluna **Last used** das chaves tem granularidade de um dia e é gravada em segundo plano na primeira autenticação do dia, então pode demorar alguns instantes. O sinal mais direto de que os dados chegaram é o painel do serviço (seção 5.2), que passa a mostrar requisições em 1 a 2 minutos, dependendo do intervalo de exportação do SDK. Se aparecer "No requests in this period yet", confira: endpoint, header `Authorization`, o nome da métrica e se a sua API realmente recebeu tráfego depois de configurar o exporter.

## 5. Ler os painéis

Os dois painéis têm o seletor de período **Last 24 hours, 7 days, 30 days, 90 days e 12 months**, padrão 7 dias. Todos os horários são **UTC**. O período termina agora e começa no início de uma hora, dia ou mês, então "Last 7 days" são os 6 dias anteriores inteiros mais o dia de hoje até agora. O período fica na URL (`?period=30d`), então dá para compartilhar o link com quem também está no projeto.

### 5.1 Painel de um site (`/sites/[siteId]`)

- **Page views, Unique visitors, Average visit duration e Bounce rate**, cada um com a variação contra o período anterior de mesmo tamanho.
- Gráfico de page views por hora, dia ou mês (conforme o período), com tabela alternativa.
- Listas de **Referrers, Countries, Devices e Browsers**.
- Visitante único é uma estimativa feita sem cookie nem identificador persistente no navegador (ver seção 7); o critério exato é do Umami e não foi verificado aqui, então não trate o número como contagem de pessoas.
- Se o Umami estiver fora, o painel mostra "Web analytics is unavailable right now"; tente de novo em alguns minutos.

### 5.2 Painel de um serviço de API (`/services/[serviceId]`)

- **Requests**, **Server errors (5xx)** (percentual e quantidade), **Median latency (p50)** e **p95 latency**.
- Gráficos de requisições (com 4xx e 5xx no detalhe) e de latência por período, com tabela alternativa.
- **Endpoints**: método e rota normalizada, com requisições, % de 4xx e 5xx, p50 e p95; mostra as **100 rotas** com mais requisições (o cabeçalho avisa "Top 100 of N").
- **Status codes**: distribuição por código.
- A aba **Ingest keys** leva ao gerenciamento de chaves (seção 4.1).

Como ler os números e os limites:

- **p50 e p95 são aproximações.** A latência é guardada em 15 baldes (14 limites, de 5 ms a 10 s, mais um balde acima de 10 s) e o percentil é interpolado dentro da faixa; o erro máximo é a largura da faixa. Serve para tendência e comparação, não para medir milissegundos. Latência acima de 10 s aparece como 10 s.
- **Atraso:** os dados novos aparecem em 1 a 2 minutos, dependendo do intervalo de exportação do SDK. Um job consolida os minutos em horas e dias a cada 15 minutos, só com horas fechadas há 5 minutos; enquanto isso o painel lê a parte recente direto dos minutos, então a diferença não deveria aparecer nos números.
- **Retenção:** minutos por 35 dias, horas e dias por 25 meses. O painel lê cada trecho da tabela que o cobre: as horas consolidadas vêm do 1h, os dias do 1d e a parte recente, ainda não consolidada, do 1m.
- **Lacunas** nos gráficos de latência são períodos sem requisições.
- **Só HTTP de servidor:** o painel não mostra clientes, IPs, corpos nem traces; isso nunca chega ao stellytics.
- **Projeto removido:** um projeto removido pelo SCF vê apenas o aviso, e a ingestão (web e API) é recusada.

## 6. Problemas comuns

| Sintoma | Causa provável |
|---|---|
| Sem page views | `data-site` errado; página em HTTP (sem `crypto.subtle`); script v1.0.0; bloqueio por ad blocker; `integrity` não bate com a versão do caminho |
| Console do navegador recusa o script | `integrity` de outra versão, ou falta `crossorigin="anonymous"` |
| Primeiro page view do Next.js sumiu | `next/script` com `lazyOnload`; ponha a tag no `<head>` do layout |
| `401` ao exportar métricas | chave errada, revogada ou de projeto removido; o header precisa ser `Authorization: Bearer ntf_...` |
| Painel de API vazio com chave válida | métrica ou spans que não são de servidor HTTP; confira `http.request.method` e `http.response.status_code` |
| Muitas rotas `{other}` | `http.route` ausente com caminhos de alta cardinalidade; informe o `http.route` |
| Perdi a chave | não dá para recuperar; crie outra e revogue a antiga |

## 7. Privacidade em resumo

- **Web:** sem cookie, sem `localStorage` nem `sessionStorage`. O gateway **trunca o IP** (último octeto no IPv4, /48 no IPv6) antes da geolocalização, e o IP completo não é gravado. Gravação de sessão e mapas de calor ficam desligados.
- **API:** só contadores e histogramas por rota, método e status; traces, spans e logs são descartados.
- Por não usar cookie, o script não exige banner de consentimento **por si só**; a decisão jurídica é do seu projeto. Os papéis, o que é gravado e os pontos de atenção estão em [`docs/privacidade.md`](privacidade.md), que não é parecer jurídico.
