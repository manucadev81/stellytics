# Umami: acesso e recursos desligados

O Umami (v3.4.1, [ADR 0001](adr/0001-ferramenta-base.md)) roda **sem nenhum patch**: a imagem oficial, fixada por tag e digest, com a configuração descrita aqui. Se um patch for necessário, ele entra nesta página, com o motivo e como reaplicá-lo a cada versão nova.

## Só na rede interna (#62)

O Umami não publica porta (`infra/compose/dev.yaml`). Só o portal e o gateway `ingest-web` falam com ele, por `http://umami:3000` na rede do Compose. Se o `/api/send` do Umami ficasse acessível, qualquer um mandaria eventos direto, sem passar pela prova de execução de JS, pelo limite por IP e pelo truncamento de IP do gateway; e a interface de administração ficaria exposta.

- **Compose:** o serviço `umami` não tem `ports`. Para desenvolvimento, `infra/compose/umami-port.yaml` publica a porta em `127.0.0.1:3001`; só o CI e a máquina de quem desenvolve usam esse arquivo.
- **Caddy (produção):** não tem rota para o Umami. Só o gateway (`/e`, `/c` e `/s/`) e o portal são publicados (ver [`infra/README.md`](../infra/README.md)).
- **Gateway:** só responde a `POST /e`, `GET /c`, `GET /s/` e `GET /healthz`. Ele repassa ao Umami apenas o `/api/send`, com o payload remontado; o `/api/record` e as outras rotas do Umami não existem nele.

O job `compose` do CI confere que o serviço não publica porta e que nenhuma porta publicada pelo ambiente responde ao `/api/send` ou ao `/api/record` como o Umami responderia.

## Gravação de sessão e heatmaps desligados

No Umami 3.4.1, gravação de sessão (replays) e heatmaps são o mesmo recurso, o "recorder":

| Peça | Onde |
|---|---|
| Liga/desliga | Por site: colunas `website.recorder_enabled` (padrão `false`) e `website.replay_config` (JSON com `replayEnabled` e `heatmapEnabled`, padrão nulo). Mudam por `POST /api/websites/:id` com `replayConfig`. Não há variável de ambiente. |
| Script do navegador | `recorder.js`, servido pelo próprio Umami |
| Rota de gravação | `POST /api/record`, que exige o token de sessão (`x-umami-cache`) devolvido pelo `/api/send` e responde `{"ok":false,"reason":"recorder_disabled"}` quando o site não tem `recorder_enabled` |
| Tabelas | `session_replay`, `session_replay_saved` e `heatmap_event`, no banco `umami` |

Ficam desligados por três motivos independentes:

1. O portal cria os sites só com `name` e `domain` (`apps/portal/src/lib/umami/provision.ts`) e nunca envia `replayConfig`, então todo site fica com `recorder_enabled = false`.
2. Nem o `recorder.js` nem o `/api/record` são alcançáveis de fora: o Umami está só na rede interna e o gateway não repassa essas rotas.
3. O script próprio (`packages/tracker`) não grava interação nenhuma; manda só page views.

O teste ponta a ponta `TestRecorderRecordsNothing` (`services/ingest-web/e2e/recorder_test.go`, no job `compose`) cria um site como o portal cria, manda page views reais pelo gateway, tenta gravar replay e heatmap pelo gateway e direto no Umami e confere que as três tabelas continuam vazias e que nenhum site tem `recorder_enabled` ou `replay_config`.

Ao subir a versão do Umami, confira se esses nomes (colunas, rota e tabelas) mudaram e ajuste o teste e esta página.
