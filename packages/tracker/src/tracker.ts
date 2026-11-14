// Lógica do script de rastreamento, separada da entrada (index.ts) para os testes.
// Não usa cookie, localStorage, sessionStorage nem identificador persistente:
// a sessão é calculada pelo Umami a partir do IP truncado pelo gateway e do User-Agent.

// Caminho do endpoint de eventos no gateway. Neutro de propósito (ver #23).
export const EVENT_PATH = "/e";

// Caminho do desafio da prova de execução de JS (#25).
export const CHALLENGE_PATH = "/c";

// O desafio vale 10 minutos no gateway; o script pede outro antes disso.
export const CHALLENGE_REFRESH_MS = 5 * 60_000;

// Limite de tentativas da prova. Com a dificuldade padrão (12 bits), a média é
// de 4 mil; passar disso indica um gateway mal configurado, e o evento é perdido.
const MAX_ATTEMPTS = 1 << 22;

// Espera antes de enviar o page view de uma navegação de SPA, para o título da
// página nova já estar no document (mesmo atraso do script do Umami).
export const NAVIGATION_DELAY_MS = 300;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Só os parâmetros de campanha saem da query string; o resto pode ter dado pessoal
// (e-mail, token de convite etc.).
const KEPT_PARAMS = /^utm_(source|medium|campaign|term|content)$/;

// Prova de execução de JS: o token do desafio e o nonce que o resolve.
export interface Proof {
  t: string;
  n: string;
}

// Payload no formato do /api/send do Umami, que o gateway valida e repassa.
// O campo p (a prova) fica no gateway.
export interface EventPayload {
  type: "event";
  p: Proof;
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

// Conta os bits zero no começo do hash.
export function leadingZeroBits(hash: Uint8Array): number {
  let n = 0;
  for (const byte of hash) {
    if (byte !== 0) return n + Math.clz32(byte) - 24;
    n += 8;
  }
  return n;
}

// Acha o nonce tal que SHA-256(token + ":" + nonce) comece com `bits` bits zero.
export async function solve(token: string, bits: number): Promise<string | null> {
  const encoder = new TextEncoder();
  for (let i = 0; i < MAX_ATTEMPTS; i++) {
    const nonce = String(i);
    const hash = await crypto.subtle.digest("SHA-256", encoder.encode(`${token}:${nonce}`));
    if (leadingZeroBits(new Uint8Array(hash)) >= bits) return nonce;
  }
  return null;
}

// Pede o desafio ao gateway e o resolve. Sem crypto.subtle (página fora de
// HTTPS) ou sem resposta do gateway, devolve nulo e os eventos não saem.
async function fetchProof(url: string): Promise<Proof | null> {
  try {
    const response = await fetch(url, { credentials: "omit" });
    if (!response.ok) return null;
    const { t, d } = (await response.json()) as { t: string; d: number };
    const n = await solve(t, d);
    return n === null ? null : { t, n };
  } catch {
    return null;
  }
}

// Inicia o rastreamento. Devolve uma função que desfaz os ganchos (usada nos testes).
export function start(script: HTMLScriptElement | null): () => void {
  const website = script?.dataset.site ?? "";
  if (!script?.src || !UUID.test(website)) return () => {};
  const endpoint = new URL(EVENT_PATH, script.src).href;
  const challenge = new URL(`${CHALLENGE_PATH}?s=${website}`, script.src).href;

  // Um desafio por carregamento de página, renovado em páginas abertas por muito tempo.
  let proof: Promise<Proof | null> | undefined;
  let proofAt = 0;
  const getProof = () => {
    if (!proof || Date.now() - proofAt > CHALLENGE_REFRESH_MS) {
      proofAt = Date.now();
      // Falhou? Esquece o resultado, e o próximo evento tenta de novo.
      proof = fetchProof(challenge).then((p) => {
        if (!p) proof = undefined;
        return p;
      });
    }
    return proof;
  };

  const send = (url: string, referrer: string) => {
    // O payload é lido agora (título, URL), antes de esperar a prova.
    const payload: EventPayload["payload"] = {
      website,
      hostname: location.hostname,
      url,
      referrer,
      title: document.title,
      language: navigator.language,
      screen: `${screen.width}x${screen.height}`,
    };
    getProof().then((p) => {
      if (!p) return;
      const body: EventPayload = { type: "event", p, payload };
      // text/plain evita o preflight de CORS; o gateway lê o corpo como JSON.
      return fetch(endpoint, {
        method: "POST",
        body: JSON.stringify(body),
        headers: { "Content-Type": "text/plain" },
        credentials: "omit",
        keepalive: true,
      });
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
