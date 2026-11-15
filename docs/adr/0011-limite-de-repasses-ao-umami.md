# 0011. Limite de repasses simultâneos ao Umami e comportamento sob saturação

- **Status:** Proposto
- **Data:** 15/11/2026
- **Issue:** ainda não criada; o texto pronto está em [`docs/relatorios/carga-1m-dia.md`](../relatorios/carga-1m-dia.md), seção "Pendência pronta para virar issue"
- **Depende de:** [ADR 0002](0002-arquitetura-e-stack.md), [ADR 0009](0009-alta-disponibilidade.md)

## Contexto

O teste de carga ([`docs/relatorios/carga-1m-dia.md`](../relatorios/carga-1m-dia.md)) achou que o Umami satura perto de 200 page views por segundo por instância (um núcleo de Node) e que o repasse do gateway (`services/ingest-web/internal/relay`) era síncrono e **sem limite de requisições em voo**: cada `POST /e` ficava pendurado até o Umami gravar. Acima da capacidade, a fila crescia dentro do gateway e do Umami, a latência do `POST /e` passava de 5 s e a memória do gateway subia, sem que nada fosse recusado. Nada disso aparece com o alvo do RFP (média de 11,6/s e pico de 116/s), mas um pico maior, ou um robô que passe pelas camadas anti-bot, faria o mesmo em produção.

Repetição da medição com a mesma ferramenta e a mesma máquina, a 400 page views/s por 60 s, **sem limite**: `POST /e` com p50 de 10,0 s (o teto do `http.Client` do repasse), 16.192 de 23.907 respostas `502` por timeout, memória do gateway de 355 MB. Os números completos, lado a lado com os de cada limite, estão em "Medição" abaixo.

Dois fatos do desenho importam:

- O script do navegador envia com `keepalive` e **não tenta de novo**. Um evento que o gateway não consegue repassar **se perde**; não há fila durável no meio.
- O repasse é a única camada do gateway que depende de um serviço externo lento. A prova de JS, o limite por IP, a consulta à `ingest_site` (cache) e o limite de fingerprints são locais e baratos.

## Decisão

Um semáforo no repasse (`relay.Limited`), controlado por duas variáveis:

- `RELAY_MAX_INFLIGHT` (0 a 100000, padrão `0`): máximo de repasses ao Umami ao mesmo tempo. **`0` desliga o limite e preserva o comportamento anterior exatamente** (o gateway nem monta o semáforo; há teste de regressão).
- `RELAY_QUEUE_WAIT` (0 a 10 s, padrão `250ms`): quanto uma requisição espera por uma vaga. Passado o prazo, ela é recusada. `0` recusa na hora se não houver vaga.

Quando não há vaga dentro do prazo, o gateway responde **`503` com `Retry-After: 1`** e **o evento é descartado: ele se perde**. O script não tenta de novo, então o `Retry-After` serve a clientes que não são o nosso script (testes, integrações, monitoramento). Perder o evento de forma explícita, contada e visível é melhor que segurar a conexão por 5 a 8 s e estourar a memória: o navegador do visitante já foi embora, a conexão e a memória do gateway não.

Regras:

- **Só o repasse é limitado.** `GET /c`, `GET /s/`, `GET /healthz`, a prova de JS, o limite por IP e o descarte por site recusado ou por fingerprint não passam pelo semáforo e respondem como sempre, mesmo com o repasse saturado. A **ordem** das camadas não muda (prova de JS, `ingest_site`, fingerprints, repasse); a espera por vaga acontece dentro do repasse.
- **A vaga volta em qualquer saída**: sucesso, erro do Umami, timeout, cancelamento do cliente e panic (liberação por `defer`). Quem cancela enquanto espera a vaga não conta como descarte por saturação.
- **Descarte por saturação não gasta a cota de fingerprints do IP.** O handler registra a fingerprint antes do repasse (ordem inalterada); se o repasse é recusado por saturação e a fingerprint tinha acabado de entrar na conta, ela é removida (`Counter.Forget`). Sem isso, num pico, visitantes legítimos teriam a cota de 50 fingerprints do /24 consumida por eventos que nunca foram gravados, e passariam a ser descartados em silêncio quando o Umami voltasse. Uma fingerprint que já estava na conta fica como estava. Há teste que falha se a remoção for tirada.
- **Contador agregado**: `Limited.Dropped()` conta os descartes. O gateway registra um aviso por minuto, só quando o total mudou (`descartados`, `total`, `em_voo`), como faz com as respostas vencidas do cache de sites. **Sem IP, `User-Agent` nem ID de site.** O operador vê a taxa de descarte nesse log e na contagem de `503` do Caddy. O último minuto parcial antes de um desligamento não é registrado.

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| **Semáforo com espera curta e `503` (escolhida)** | Memória e latência limitadas; o descarte é contado e visível no status HTTP; a espera curta absorve rajadas de milissegundos sem perder evento; simples, em memória, sem estado a replicar | O evento se perde (o script não tenta de novo); durante a saturação, um `503` só sai para site aceito com prova válida, o que distingue site existente de desconhecido (ver Consequências) |
| Responder `204` e descartar em silêncio (como site desconhecido) | Não dá pista nenhuma a quem testa; o script não vê erro | Esconde a perda de quem monitora o status HTTP: a taxa de descarte só existiria no log do gateway. O visitante já não vê a resposta de qualquer forma, então a vantagem é só contra sondagem |
| Fila limitada maior (esperar segundos por vaga) | Perde menos eventos em rajadas curtas | Reintroduz o problema em escala menor: conexões e memória seguradas por segundos, e a latência de quem é aceito sobe (medido: com 256 vagas e espera de 250 ms o p50 dos aceitos foi de 1,6 s). Só vale se a rajada for mais curta que o prazo |
| Fila durável (disco, Redis, NATS) entre o gateway e o Umami | Não perde evento num pico | Novo componente para operar, backup e consistência; contradiz o gateway sem estado do ADR 0002; o pico acima da capacidade média só adia o problema, a fila cresce sem fim se a taxa de entrada continuar maior que a de saída |
| Escalar o Umami na horizontal (várias instâncias atrás do gateway) | É a única opção que aumenta a capacidade em vez de escolher o que perder | O Umami é um nó de Node por instância; precisa de balanceamento no gateway ou no Caddy e ajuste do pool do Postgres; mais VMs e custo; ainda precisa de limite (qualquer capacidade finita satura). **Complementar, não substituta**: o limite deve existir de qualquer jeito. Fica para o ADR 0009 (alta disponibilidade) |
| Limite por site (um tenant não esgota as vagas dos outros) | Evita que um site sob ataque ou em pico use todas as vagas | Ver "Limite por site" abaixo |
| Só baixar o timeout do `http.Client` (hoje 10 s) | Uma linha | Não limita a memória: com 400/s e timeout de 2 s ainda há 800 requisições em voo; descarta depois de gastar a vaga no Umami, e o Umami continua processando o que o gateway já abandonou (medido: 19.881 eventos gravados contra 7.715 `204` na execução sem limite) |

### Limite por site (o ponto multi-tenant)

O semáforo é **global ao processo**: um tenant em pico, ou alguém que mande eventos com o ID público do site dele, pode ocupar todas as vagas e fazer o gateway recusar eventos de outros tenants. Isso é real: o ID do site não é segredo.

O que já limita o estrago, sem nada novo: o limite de taxa por IP truncado (`RATE_LIMIT`), a prova de JS e o limite de fingerprints por IP e site (`FINGERPRINT_LIMIT`). Eles seguram um atacante de um /24; **não seguram um pico legítimo e grande de um único tenant**, nem um robô distribuído por muitos IPs que resolva a prova de JS.

**Não implementado neste ADR**, por duas razões: (1) com o Umami como único gargalo, o recurso escasso é a capacidade dele, e uma cota fixa por site desperdiça vaga quando os outros estão ociosos; a solução correta é uma partilha justa (cada site com um piso reservado e o resto compartilhado, ou fila por site com rodízio), com estado por site e mais pontos de falha, e não é pequena; (2) a escala atual (poucos projetos, alvo de 11,6/s) não exerce o problema, e não medimos a disputa entre tenants. **Pendência registrada**: se um tenant isolado passar a competir pelas vagas, a próxima peça é uma reserva por site (por exemplo, nenhum site pode ocupar mais que uma fração das vagas quando outros esperam) em cima do mesmo `relay.Limited`. O contador atual não separa descartes por site (e não deve gravar ID de site em log de texto); a medição por site precisaria de métrica própria.

## Medição

Máquina local, Postgres 17.11 e Umami 3.4.1 em contêineres, gateway como binário Go no host, gerador `services/loadtest`, 400 page views/s por 60 s (Poisson), banco do Umami vazio no começo e crescendo entre as execuções, mesma configuração do relatório de carga (`PROOF_BITS=12`, `FINGERPRINT_LIMIT=50`, `RATE_LIMIT=20`). **Uma execução por linha; sem repetição, então a variação entre execuções não está medida.** Outros programas rodavam na máquina. `Aceitos` são os `204`; latências em milissegundos vistas pelo gerador.

| Configuração | `204` | `503` / `502` | p50 / p95 / p99 dos aceitos | p50 das recusas | Memória do gateway (pico) | Gravados no Umami |
|---|---|---|---|---|---|---|
| Sem limite | 7.715 | 502: 16.192 | 5.558 / 9.333 / 9.858 ms | 502 em 10.001 ms | 355 MB | 19.881 (inclui os 502 que o Umami gravou depois) |
| `RELAY_MAX_INFLIGHT=256`, espera 250 ms | 11.324 | 503: 12.838 | 1.570 / 1.810 / 3.784 ms | 251 ms | 58 MB | 11.324 de 11.324 |
| `RELAY_MAX_INFLIGHT=64`, espera 250 ms | 12.048 | 503: 12.166 | 558 / 636 / 673 ms | 251 ms | 46 MB | 12.083 de 12.048 |
| `RELAY_MAX_INFLIGHT=16`, espera 250 ms | 9.774 | 503: 14.254 | 332 / 433 / 511 ms | 251 ms | 40 MB | 9.774 de 9.774 |
| `RELAY_MAX_INFLIGHT=16`, espera 50 ms | 11.127 | 503: 12.688 | 128 / 166 / 202 ms | 51 ms | 31 MB | 11.127 de 11.127 |
| `RELAY_MAX_INFLIGHT=16`, espera 250 ms, **116/s** (pico do RFP) | 6.977 | 0 | 28 / 88 / 166 ms | n/a | 25 MB | 6.977 de 6.977 |

Leitura:

- Sem limite o gargalo reproduz: 5 a 10 s de latência, mais de dois terços das respostas em `502` por timeout e memória de 355 MB. Com limite, a memória do gateway ficou entre 31 e 58 MB, e **todo evento que recebeu `204` foi gravado** (as execuções limitadas gravaram exatamente os `204`, com 35 eventos a mais numa delas, que atribuímos ao fim da fila da execução anterior; não investigamos).
- O limite não reduziu o que o Umami grava por segundo: de 9.774 a 12.048 aceitos em 60 s (163 a 201/s), contra 7.715 `204` sem limite (os outros foram `502` para o navegador). O Umami trabalha na mesma taxa; a diferença é que o gateway deixa de segurar o que ele não dá conta e recusa cedo.
- **A latência dos aceitos é a espera por vaga mais o tempo no Umami**; ela cai com a espera mais curta (`RELAY_QUEUE_WAIT=50ms`: p99 de 202 ms) e com menos vagas. O número de vagas ótimo é pequeno (16 a 64 para uma instância); 256 já deixa a fila acumular dentro do Umami (p50 de 1,6 s). A regra prática: `RELAY_MAX_INFLIGHT` próximo de 2 a 4 vezes o número de requisições que o Umami atende em paralelo sem fila, e `RELAY_QUEUE_WAIT` curto.
- **No pico do RFP (116/s) com 16 vagas e a espera padrão não houve nenhum `503`.** O limite só age acima da capacidade.
- Como o limite não eleva a capacidade, a 400/s de 50% a 59% dos eventos se perdem em qualquer configuração. O limite troca "tudo lento e incerto" por "parte recusada de forma clara"; só mais capacidade (mais instâncias do Umami) reduz a perda.

**Não foi testado:** várias instâncias do gateway (o semáforo é por processo; N gateways permitem N vezes `RELAY_MAX_INFLIGHT` ao mesmo Umami), produção (Caddy, TLS, rede real, Hetzner), Umami com muitos dados (o banco começou vazio), execuções longas, e disputa entre tenants (um site só no teste). Os valores `16`, `64` e `256` foram escolhidos para a medição, não calculados para produção.

## Consequências

- **Padrão `0` (desligado)**: nada muda para quem não ligar a variável. Em produção, o `prod.yaml` ainda **não repassa** `RELAY_MAX_INFLIGHT` nem `RELAY_QUEUE_WAIT`; ligar exige uma mudança à parte nele e no Ansible.
- Um evento recusado por saturação se perde para sempre. A métrica de pageviews do dashboard fica subcontada durante a saturação e **o painel não avisa**; o aviso por minuto no log e a contagem de `503` no proxy são os sinais. Quem liga o limite deve alertar sobre isso (sugestão: `503` do `POST /e` acima de uma pequena fração por alguns minutos).
- Durante a saturação, um `503` só sai para site aceito com prova válida; um site desconhecido continua recebendo `204`. Isso deixa distinguir, **apenas enquanto o gateway está saturado**, um ID de site existente de um inexistente, contra a regra do spec do E2 (seção 6) de não dar pista. Aceitamos porque o ID do site é público (está no HTML de quem instala o script, ver `docs/anti-bot-e-cadeia-de-suprimentos.md`) e a pista exige o fluxo completo (desafio e prova). Se isso não for aceitável, a alternativa é responder `204` também na saturação, ao custo de esconder a perda do status HTTP.
- O semáforo é por processo. Com mais de uma instância do gateway, o limite ao Umami é a soma.
- A capacidade real continua a do Umami (cerca de 200 page views/s por instância nesta máquina). Escalar o Umami fica como evolução (ADR 0009).
- Respostas do `POST /e` que o operador passa a ver: `503` com corpo `overloaded` e `Retry-After`, distinto do `503` de falha da `ingest_site` (corpo `service unavailable`).
