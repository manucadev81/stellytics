# Teste de carga: 1 milhão de requisições por dia (#51)

Data: 2026-11-15. Ferramenta: `services/loadtest` (ver o `README.md` da pasta).
Todos os números abaixo saíram das execuções descritas aqui; nada foi
extrapolado sem estar marcado como estimativa.

## Resumo

- **Carga alvo atendida nesta máquina local, sem Caddy, TLS, contêineres nem hardware Hetzner; isto não é prova de produção.** A média de 1M/dia é 11,6/s; rodamos 10 min nessa taxa, 5 min a 10x (116/s) e 5 min de volta à média, com web e API ao mesmo tempo.
- **Web:** 45.029 page views, 100% gravados no Umami, zero erro, zero 429. Latência do `POST /e` no pico de 10x: p50 8,1 ms, p95 63 ms, p99 244 ms.
- **API:** 1.051.022 requisições simuladas em 4 minutos (média de 4.379/s, pico de 10.000/s, ou seja, 1M/dia inteiro em 4 min), por spans. Tudo gravado em `api_metric_1m`, contagem exata, coletor a 0,08 núcleo.
- **Gargalo achado: o Umami**, não o gateway nem o coletor. Ele satura perto de 200 page views/s (um núcleo de Node). Acima disso a latência do `POST /e` passa de 5 s e o gateway acumula requisições na memória, sem recusar (pendência 1).
- **Disco:** o Umami cresceu cerca de 0,9 KB por page view (estimativa de 0,9 GB/dia a 1M page views/dia; ver "Disco"). As métricas de API são baratas (181 bytes por linha de 1 minuto).
- Nada foi alterado no pipeline. A seção "Gargalos e pendências" lista 3 itens: uma pendência de produto (Umami e gateway sob saturação, com texto de issue pronto), um comportamento esperado (nomes de rota duplicados em spans) e um erro corrigido na própria ferramenta de carga.

## O que significa "1 milhão de requisições por dia"

1M/dia = 11,57/s em média. Tráfego real não é uniforme, então além da média
usamos **10x a média** (116/s) por 5 minutos como pico. O racional: sites de
projetos do ecossistema Stellar têm picos de lançamento, airdrop e notícias que
costumam concentrar várias vezes a média em poucos minutos; 10x cobre isso e é
um número fácil de conferir. Depois do pico, uma volta à média mostra se o
sistema se recupera. Em seguida, degraus crescentes mostram onde cada pipeline
satura.

A unidade muda por pipeline:

- **Web:** 1 requisição = 1 page view. Cada um custa 2 chamadas ao gateway (`GET /c` do desafio e `POST /e`), ou seja, 1M page views/dia = 2M chamadas HTTP/dia ao gateway.
- **API:** 1 requisição = 1 requisição HTTP que o backend do tenant atendeu. Quem manda ao coletor não é cada requisição: o SDK do OTel agrega em histogramas e exporta a cada N segundos (modo `metrics`, o caminho normal e o mais leve) ou manda um span por requisição (modo `spans`, o pior caso para o coletor). Testamos os dois.

## Ambiente

- Máquina de desenvolvimento local, **não representa a Hetzner de produção**: Intel Core i5-12450H (12 threads), 15 GiB de RAM, Linux, rede por loopback. Outros programas e agentes rodavam na mesma máquina (carga de 0,92 no minuto anterior à execução principal), o que torna as medições um pouco ruidosas.
- PostgreSQL 17.11 e Umami 3.4.1 (as imagens do `dev.yaml`) em contêineres do Compose, projeto `notify51`, sem limite de CPU ou memória, `shared_buffers` padrão de 128 MB.
- **Gateway (`ingest-web`), coletor e `rollup` rodaram como binários Go no host**, não em contêineres: o Docker da máquina não tem BuildKit e os Dockerfiles usam `--mount=type=cache`. Os binários são os do repositório (`go build -trimpath`), com as mesmas variáveis do `dev.yaml`: `PROOF_BITS=12`, `RATE_LIMIT=20`, `RATE_BURST=40`, `FINGERPRINT_LIMIT=50`, `SITE_CACHE_TTL=5s`, `INGEST_KEY_CACHE_TTL=5s`, `TRUSTED_PROXIES=127.0.0.0/8` (para o gerador mandar IPs distintos no `X-Forwarded-For`). O coletor usou uma cópia do `config.yaml` só com as portas trocadas (14317/14318).
- CPU e memória: dos binários, por `/proc` (CPU em núcleos, 1,0 = um núcleo inteiro; memória é o RSS); dos contêineres, por `docker stats`. Amostras a cada 2 s.
- As migrações do portal foram aplicadas direto com `psql` (o portal não subiu), as mesmas da pasta `apps/portal/drizzle`.

## Método

`loadtest setup` cria no Umami um site e, no banco `notify`, um tenant com esse site e outro tenant com 4 serviços de API que mandam métricas e 2 que mandam só spans (cada um com a própria chave `ntf_...`, criada como o `compose-check.sh` faz). Depois:

- **web:** chegada de Poisson (o ritmo não espera as respostas). Cada page view faz `GET /c`, resolve a prova de JS (SHA-256 com 12 bits, mesmo algoritmo do `internal/proof`, repetido no gerador porque o Go não deixa importar um pacote `internal` de outro diretório) e manda `POST /e` com `Content-Type: text/plain`. 20.000 visitantes simulados, cada um com IP em um /24 próprio (via `X-Forwarded-For`), UA, idioma e tela fixos, para o limite por IP e o de fingerprints se comportarem como com visitantes diferentes. No fim, conta as linhas de `website_event` do site no banco do Umami e compara com os `204` recebidos. Como o gateway responde 204 também ao descartar, **só essa contagem diz se o evento chegou**.
- **api:** simula os serviços, que exportam OTLP/HTTP em protobuf com `Authorization: Bearer`. Rotas com e sem `http.route` (as sem passam pela normalização do coletor), status 95% 200 / 3% 404 / 1,5% 400 / 0,5% 500, latência lognormal de mediana 40 ms. Métricas: histogramas delta a cada 10 s (o SDK usa 60 s por padrão; 10 s é mais exigente). Spans: lotes de até 512 ou a cada 1 s. No fim, compara `sum(request_count)` de `api_metric_1m` por serviço com o que estava em exportações que receberam 2xx.

Execuções: (1) principal, web e API ao mesmo tempo, perfil `11.6x10m,116x5m,11.6x5m`; (2) degraus do web sozinho, `50,100,200,400,800` page views/s por 1 min cada; (3) degraus da API só com spans, `500,2000,5000,10000`/s por 1 min cada; (4) `api_metric_maintain` (rollup) medido com dados reais e com um volume de 35 dias semeado; (5) tamanhos em disco.

## Resultados

### 1. Execução principal (web e API juntos, 20 min)

Web, page views iniciados: 6.943 (média 11,57/s) + 34.671 (115,6/s) + 3.415 (11,4/s) = 45.029. Latência em ms ou µs vista pelo gerador, na loopback.

| degrau | operação | n | p50 | p95 | p99 | máx | códigos |
|---|---|---|---|---|---|---|---|
| 11,6/s, 10 min | `GET /c` | 6.943 | 0,42 ms | 1,27 ms | 1,53 ms | 4,63 ms | 200=6.943 |
| | `POST /e` | 6.943 | 11,71 ms | 25,75 ms | 52,7 ms | 163 ms | 204=6.943 |
| 116/s, 5 min | `GET /c` | 34.671 | 0,23 ms | 0,62 ms | 0,99 ms | 21,5 ms | 200=34.671 |
| | `POST /e` | 34.671 | 8,14 ms | 63,17 ms | 244,07 ms | 660 ms | 204=34.671 |
| 11,6/s, 5 min | `GET /c` | 3.415 | 0,84 ms | 1,35 ms | 1,54 ms | 2,54 ms | 200=3.415 |
| | `POST /e` | 3.415 | 12,59 ms | 24,41 ms | 27,9 ms | 69,9 ms | 204=3.415 |

- Erros de rede ou de desafio: 0. Respostas 429: 0. Saturação do gerador: 0.
- **Gravados no Umami: 45.029 de 45.029 (100%); descartados ou perdidos: 0.**
- O `POST /e` espera o Umami gravar (repasse síncrono), por isso a latência dele é a do Umami; o `GET /c` só assina um token.
- A prova de JS no cliente custou p50 0,3 a 1 ms e p99 até 6,9 ms (CPU do gerador, desktop; não mede celular).

API: 45.454 requisições simuladas (4 serviços em métricas, 2 em spans), 1.028 + 600 + 526 exportações de spans e 236 + 120 + 124 de métricas, **todas 200**; latência do coletor por exportação p99 de 2,4 a 4,2 ms (métricas) e de 1,9 a 3,7 ms (spans). Exportações com falha: 0.

| serviço | modo | enviadas com 2xx | gravadas em `api_metric_1m` |
|---|---|---|---|
| 4 serviços | métricas | 7.607 / 7.683 / 7.550 / 7.534 | 7.607 / 7.683 / 7.550 / 7.534 |
| 2 serviços | spans | 7.514 / 7.566 | 7.514 / 7.566 |
| total | | 45.454 | 45.454 |

**Nota de honestidade:** a saída bruta dessa execução mostrou 45.634 gravadas (+180). Foi a soma de um teste de fumaça de 10 s que rodei antes, com os mesmos serviços (34+28+42+22+28+26 = 180, por serviço exatamente a diferença). A ferramenta contava o total do serviço no banco, sem descontar o que já havia; corrigi para medir a linha de base antes do teste e conferi com uma execução limpa depois (`11.6x2m`: 1.312 simuladas, 1.312 enviadas, 1.312 gravadas). Nenhuma linha duplicada ou perdida.

Em todas as linhas de `api_metric_1m` do tenant no fim (3.984.673; as 3.983.970 de outros trechos do relatório foram contadas em outro momento, antes da execução limpa de 2 min e da de degraus; isso inclui o volume semeado, ver abaixo), os buckets somam a contagem e a soma de duração é positiva. Rota `{other}` (teto de 500 rotas): 0.

Recursos na execução principal (média / pico em núcleos; memória de pico):

| processo | 11,6/s | 116/s | 11,6/s |
|---|---|---|---|
| gateway | 0,01 / 0,03 (23 MB) | 0,07 / 0,09 (40 MB) | 0,02 / 0,07 (40 MB) |
| coletor | 0,01 / 0,02 (43 MB) | 0,00 / 0,01 (21 MB) | 0,01 / 0,02 (20 MB) |
| Umami (contêiner) | 0,10 / 0,30 (262 MB) | 0,46 / 0,64 (304 MB) | 0,12 / 0,31 (298 MB) |
| Postgres (contêiner) | 0,04 / 0,10 (60 MB) | 0,17 / 0,28 (104 MB) | 0,04 / 0,16 (99 MB) |

### 2. Degraus do web (onde satura)

Perfil `50,100,200,400,800` page views/s, 1 min cada, `-max-inflight 2000`.

| taxa | `POST /e` p50 | p95 | p99 | máx | page views feitos | Umami (núcleos, média) |
|---|---|---|---|---|---|---|
| 50/s | 10,2 ms | 29,8 ms | 56,9 ms | 123 ms | 2.930 | 0,27 |
| 100/s | 8,0 ms | 33,1 ms | 50,2 ms | 94,8 ms | 5.940 | 0,45 |
| 200/s | 8,9 ms | 428 ms | 716 ms | 894 ms | 11.929 | 0,69 |
| 400/s | 5,48 s | 7,05 s | 7,20 s | 7,26 s | 21.646 | 1,08 |
| 800/s | 5,65 s | 7,78 s | 7,94 s | 8,05 s | 19.528 | 1,06 |

- Ainda 100% gravado no Umami (61.973 de 61.973), sem 429 nem erro. Mas a partir de 200/s o p95 já passa de 400 ms, e de 400/s o gateway fica 5 a 8 s esperando o Umami.
- O gerador atingiu o teto de 2.000 em andamento e deixou de iniciar 30.707 chegadas (as de 400/s e 800/s). O que passou dos 200/s sustentados foi a fila do degrau anterior sendo drenada, não capacidade sustentada: no total, 61.973 page views em 302 s ≈ 205/s. **Capacidade sustentada medida do pipeline web nesta máquina: cerca de 200 page views/s**, ~17x a média do alvo e ~1,7x o pico de 10x. O Umami chegou a 1,0 a 1,4 núcleo (o Node é de um só fio) e 1,37 GB; o gateway a 0,14 núcleo e 220 MB (por causa das requisições em espera); o Postgres a 0,36 núcleo.

### 3. Degraus da API (só spans, o pior caso)

Perfil `500,2000,5000,10000` requisições/s, 1 min cada, 2 serviços.

| taxa | exportações | p50 | p95 | p99 | coletor (núcleos média / pico) | memória do coletor |
|---|---|---|---|---|---|---|
| 500/s | 118 | 1,56 ms | 4,0 ms | 4,8 ms | 0,01 / 0,02 | 21 MB |
| 2.000/s | 276 | 1,56 ms | 3,1 ms | 3,8 ms | 0,02 / 0,03 | 22 MB |
| 5.000/s | 612 | 1,36 ms | 2,6 ms | 3,7 ms | 0,04 / 0,05 | 24 MB |
| 10.000/s | 1.212 | 1,21 ms | 2,5 ms | 3,6 ms | 0,07 / 0,08 | 25 MB |

- Total: **1.051.022 requisições simuladas em 4 min; enviadas 1.051.022; gravadas 1.051.022** (525.190 e 525.832 por serviço). Exportações com falha: 0.
- O coletor não saturou até 10.000/s (860x a média do alvo); o limite aqui foi o que o gerador consegue emitir, não o coletor. Não achamos o teto do coletor.

### 4. Consolidação e retenção (`api_metric_maintain`)

- Dados reais (2.370 linhas de 1 min, hora ainda aberta): 13 ms; o serviço `rollup` rodou duas vezes sozinho (24 ms e 7,4 ms). Como só horas fechadas há 5 min entram, nada foi consolidado nessa hora.
- Para medir uma carga de verdade, **semeei por SQL** 3.981.600 linhas: as 2.370 linhas reais repetidas 1.680 vezes com `bucket_start` deslocado de 30 em 30 min, cobrindo 35 dias (a retenção do 1m). **É volume sintético com a forma dos dados reais**, equivale a cerca de 2,2M requisições/dia e 114 mil linhas/dia (o dobro da carga alvo; o perfil real tinha 6 serviços e 12 rotas). Com 3.983.970 linhas em `api_metric_1m`: `api_metric_maintain(48h)` levou **2,55 s** na primeira vez e **2,58 s** na seguinte, sem nada mais a mover da partição default; os dois tempos são, portanto, o custo de consolidar as 48 h. Resultado: 11.376 linhas em `api_metric_1h` e 711 em `api_metric_1d`, ambas somando 4.380.864 requisições.
- Isso é bem menor que o intervalo de 15 min do job.

### 5. Disco

- `api_metric_1m`: 689 MiB (`pg_size_pretty`) para 3.983.970 linhas (3.981.600 semeadas + 2.370 reais) com índices, ≈ 181 bytes por linha. As 2.370 linhas reais sozinhas ocupavam 1.496 KiB, ≈ 646 bytes por linha: em tabela pequena pesa o overhead de página e de índice, então o número de 181 B/linha vale para volume grande, não para o começo do dia. `api_metric_1h`: 7,8 MB para 11.376 linhas. No perfil real, 2.370 linhas (1.496 kB) para 45.634 requisições, ou seja ≈ 5% de linhas por requisição (depende da cardinalidade de rota/status/serviço, não do volume).
- Umami: 107.197 eventos gravados nas execuções (195 do teste de fumaça + 45.029 + 61.973). O banco `umami` passou de 10,1 MB para 108 MB; `website_event` com índices ocupa 86 MB (≈ 800 bytes por page view) e `session` 12 MB. **Estimativa** (não medida): ~0,9 KB por page view no banco inteiro, ou seja ~0,9 GB por dia a 1M page views/dia, ~330 GB por ano. Os dados são sintéticos (10 caminhos, títulos curtos), então a conta real varia.

## Gargalos e pendências

1. **Umami é o teto do web (~200 page views/s por instância) e o gateway não recusa nada quando o Umami fica lento.** O repasse é síncrono e sem limite de requisições em andamento; a 400/s a latência do `POST /e` foi de 5 a 8 s e o gateway foi de 40 MB a 220 MB. Em produção isso significaria navegadores segurando conexões e, com um pico maior, risco de memória. **Nada foi alterado**; a decisão (limitar relays simultâneos e responder 503 ou 204 com descarte, fila limitada, ou mais instâncias do Umami) merece issue própria. Para o alvo do RFP (média 11,6/s, pico 116/s) não é preciso mudar nada.
2. **Dois nomes para a mesma rota em spans:** spans com `http.route` do cliente gravam `/accounts/{account_id}`; sem ele a normalização grava `/accounts/{address}`. É o comportamento especificado (o `http.route` do cliente vale como veio), mas aparece como 12 rotas distintas onde havia 9 endpoints. Vale lembrar na documentação de onboarding; não é defeito.
3. Ferramenta: a verificação de `api` não descontava linhas já existentes (corrigido, ver acima).

O que foi ajustado: nada no pipeline. Os parâmetros do `dev.yaml` (`RATE_LIMIT`, `FINGERPRINT_LIMIT`, `PROOF_BITS`) foram mantidos e não atrapalharam o teste porque cada visitante simulado tem um /24 e uma fingerprint próprios.

## Limites: o que este teste não prova

- **Produção.** Máquina local de 12 threads, sem a CPU, o disco e o Postgres da Hetzner; sem Caddy, TLS, nem o `prod.yaml`; gateway, coletor e `rollup` fora de contêiner (sem limites de recurso). Os núcleos medidos servem para comparar etapas, não para dimensionar a VM.
- **Rede real.** Cliente, gateway e coletor falaram por loopback: latências de 1 a 10 ms não incluem RTT de internet, handshake TLS nem conexões lentas. Não testamos gRPC, só OTLP/HTTP.
- **Umami com dados de 24 meses.** O banco do Umami tinha ~100 MB. Consultas dos painéis (`/stats`, `/metrics`) com centenas de GB não foram testadas, nem o efeito do tamanho nas gravações. O volume de 35 dias do `api_metric_1m` foi semeado (repetição de dados reais), e o 1h e o 1d só têm 48 h de consolidação.
- **Tempo.** Nenhuma execução durou um dia. O mais longo foi de 20 min; não há soak de horas (vazamentos lentos, vacuum, crescimento de partição, rotação de chaves, reinício do coletor).
- **Tráfego.** Visitantes sintéticos: 20.000 IPs/fingerprints, 10 páginas, nenhum bot, nenhum IPv6, nenhum navegador real (Playwright já cobre isso no e2e). Não exercitamos os limites por IP (429) nem o de fingerprints. A taxa de descarte de eventos por prova de JS ou fingerprint foi 0 porque o gerador sempre faz tudo certo.
- **API.** Métricas e spans de 6 serviços de 1 tenant; não testamos muitos tenants, cumulativo (só delta), D9 (serviços que mandam métrica e span ao mesmo tempo) nem o teto de 500 rotas. Os spans foram emitidos em lotes de 512, ou 1 s; o OTel real varia.
- **Leitura.** Os painéis do portal (consultas a `api_metric_*` e ao Umami) não foram medidos sob carga de escrita.
- O pacote `internal/proof` não pôde ser importado; o algoritmo do gerador foi copiado. Se o gateway mudar o algoritmo, o teste mostraria zero eventos gravados, não um falso positivo.

## Pendência pronta para virar issue

**Título:** `[ingest-web] limitar relays simultâneos ao Umami e definir comportamento sob saturação`

**Texto:**

> Na carga da #51 (docs/relatorios/carga-1m-dia.md) o Umami satura perto de 200 page views/s por instância (um núcleo de Node). O gateway repassa o `POST /e` de forma síncrona e sem limite de requisições em andamento: a 400/s a latência do `POST /e` foi de 5 a 8 s e o gateway foi de 40 MB para 220 MB de memória, sem recusar nada. Para o alvo do RFP (média de 11,6/s, pico de 116/s) não há problema, mas um pico maior seguraria conexões de navegadores e a memória do gateway.
>
> O que fazer: limitar os relays simultâneos ao Umami (semáforo) e definir o comportamento quando o limite estoura: responder 503 (o script não tenta de novo), responder 204 descartando o evento (como já fazemos com site não cadastrado), usar uma fila limitada, ou colocar mais instâncias do Umami atrás do gateway. Incluir timeout no repasse e métrica de eventos descartados por saturação.
>
> Critérios de aceite: com o gerador de `services/loadtest` a 400 page views/s, a latência do `POST /e` não passa de X s, a memória do gateway fica abaixo de Y MB e o descarte aparece em log ou métrica; o teste do relatório é repetido e anexado.

## Como reproduzir

```sh
# Postgres + Umami (portas no .env: POSTGRES_PORT, UMAMI_PORT)
docker compose -p notify51 -f compose.yaml -f infra/compose/umami-port.yaml up -d --wait --no-deps postgres umami
# migrações do portal: aplique apps/portal/drizzle/*.sql como o usuário notify (ou suba o portal)
cd services
go build -o /tmp/ingest-web ./ingest-web && go build -o /tmp/collector ./collector/cmd/notify-collector && go build -o /tmp/loadtest ./loadtest
# subir os dois com as variáveis do dev.yaml (TRUSTED_PROXIES=127.0.0.0/8 no gateway), depois:
/tmp/loadtest setup -out state.json
/tmp/loadtest web -state state.json -profile "11.6x10m,116x5m,11.6x5m" -watch gateway=<pid>,collector=<pid> -docker <contêineres>
/tmp/loadtest api -state state.json -profile "11.6x10m,116x5m,11.6x5m"
/tmp/loadtest teardown -state state.json
```

### Degraus, semeadura, rollup e tamanhos

```sh
# degraus do web (ceiling) e da API (spans)
/tmp/loadtest web -state state.json -profile "50x1m,100x1m,200x1m,400x1m,800x1m" -max-inflight 2000 \
  -watch gateway=<pid>,collector=<pid> -docker notify51-postgres-1,notify51-umami-1 -settle 30s
/tmp/loadtest api -state state.json -modes spans -profile "500x1m,2000x1m,5000x1m,10000x1m" \
  -watch gateway=<pid>,collector=<pid> -docker notify51-postgres-1 -settle 30s
```

Semeadura sintética de 35 dias (repete as linhas reais, uma cópia a cada 30 min para trás; rodar como o usuário `notify`, depois do degrau principal; deu 3.981.600 linhas em 22,4 s):

```sql
INSERT INTO api_metric_1m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                           request_count, duration_sum, duration_buckets)
SELECT m.bucket_start - make_interval(mins => 30 * g), m.tenant_id, m.api_service_id, m.route,
       m.method, m.status_code, m.request_count, m.duration_sum, m.duration_buckets
FROM api_metric_1m m, generate_series(1, 1680) g;
```

Rollup e tamanhos:

```sql
\timing on
SELECT api_metric_maintain(interval '48 hours');   -- 2,55 s e 2,58 s
SELECT count(*), sum(request_count) FROM api_metric_1h;
SELECT count(*), sum(request_count) FROM api_metric_1d;
SELECT pg_size_pretty(sum(pg_total_relation_size(c.oid))) FROM pg_class c
 WHERE relname ~ '^api_metric_1m_p' AND relkind = 'r';          -- 689 MiB
SELECT pg_size_pretty(pg_database_size('umami'));                -- 108 MiB (banco do Umami)
-- no banco umami: SELECT pg_size_pretty(pg_total_relation_size('website_event'));  -- 86 MiB
```

Os tamanhos do Postgres usam `pg_size_pretty` (KiB, MiB). Os IPs dos visitantes simulados ficam em 10.0.0.0/8 (a primeira execução do relatório usou 11.x.x.x, endereços públicos, só como valores de cabeçalho; nenhum pacote saiu da máquina).
