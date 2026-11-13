// Lógica do script de rastreamento, separada da entrada (index.ts) para os testes.
// Não usa cookie, localStorage, sessionStorage nem identificador persistente:
// a sessão é calculada pelo Umami a partir do IP truncado pelo gateway e do User-Agent.

// Caminho do endpoint de eventos no gateway. Neutro de propósito (ver #23).
export const EVENT_PATH = "/e";

// Espera antes de enviar o page view de uma navegação de SPA, para o título da
// página nova já estar no document (mesmo atraso do script do Umami).
export const NAVIGATION_DELAY_MS = 300;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Só os parâmetros de campanha saem da query string; o resto pode ter dado pessoal
// (e-mail, token de convite etc.).
const KEPT_PARAMS = /^utm_(source|medium|campaign|term|content)$/;

// Payload no formato do /api/send do Umami, que o gateway valida e repassa.
export interface EventPayload {
  type: "event";
  payload: {
    website: string;
    hostname: string;
    url: string;
    referrer: string;
    title: string;
    language: string;
    screen: string;
  };
}

export function pageUrl(location: Location | URL): string {
  const kept = new URLSearchParams();
  new URLSearchParams(location.search).forEach((value, key) => {
    if (KEPT_PARAMS.test(key)) kept.append(key, value);
  });
  const query = kept.toString();
  return location.pathname + (query ? `?${query}` : "");
}

// Referenciador sem query string nem fragmento; vazio quando é a própria página.
export function cleanReferrer(referrer: string, hostname: string): string {
  if (!referrer) return "";
  try {
    const url = new URL(referrer);
    if (url.hostname === hostname) return "";
    return url.origin + url.pathname;
  } catch {
    return "";
  }
}

// Inicia o rastreamento. Devolve uma função que desfaz os ganchos (usada nos testes).
export function start(script: HTMLScriptElement | null): () => void {
  const website = script?.dataset.site ?? "";
  if (!script?.src || !UUID.test(website)) return () => {};
  const endpoint = new URL(EVENT_PATH, script.src).href;

  const send = (url: string, referrer: string) => {
    const body: EventPayload = {
      type: "event",
      payload: {
        website,
        hostname: location.hostname,
        url,
        referrer,
        title: document.title,
        language: navigator.language,
        screen: `${screen.width}x${screen.height}`,
      },
    };
    // text/plain evita o preflight de CORS; o gateway lê o corpo como JSON.
    fetch(endpoint, {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "Content-Type": "text/plain" },
      credentials: "omit",
      keepalive: true,
    }).catch(() => {});
  };

  let current = pageUrl(location);
  send(current, cleanReferrer(document.referrer, location.hostname));

  // Navegação de SPA: um page view por mudança de rota, com a rota anterior como referenciador.
  let timer: ReturnType<typeof setTimeout> | undefined;
  const onNavigate = () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      const next = pageUrl(location);
      if (next === current) return;
      const previous = current;
      current = next;
      send(next, location.origin + previous.split("?")[0]);
    }, NAVIGATION_DELAY_MS);
  };

  const { pushState, replaceState } = history;
  for (const [method, original] of [["pushState", pushState], ["replaceState", replaceState]] as const) {
    history[method] = function (this: History, ...args: Parameters<History["pushState"]>) {
      const result = original.apply(this, args);
      onNavigate();
      return result;
    };
  }
  addEventListener("popstate", onNavigate);

  return () => {
    clearTimeout(timer);
    history.pushState = pushState;
    history.replaceState = replaceState;
    removeEventListener("popstate", onNavigate);
  };
}
