import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanReferrer, NAVIGATION_DELAY_MS, pageUrl, start, type EventPayload } from "../src/tracker.ts";

const SITE = "6f1c2b9e-4a3d-4c1e-9b7a-2d5e8f0a1b3c";

function script(attrs: Record<string, string>): HTMLScriptElement {
  const el = document.createElement("script");
  for (const [key, value] of Object.entries(attrs)) el.setAttribute(key, value);
  return el;
}

function sent(fetchMock: ReturnType<typeof vi.fn>): EventPayload[] {
  return fetchMock.mock.calls.map(([, init]) => JSON.parse((init as RequestInit).body as string));
}

describe("pageUrl", () => {
  it("mantém só os parâmetros utm_*", () => {
    expect(pageUrl(new URL("https://a.example/docs?email=x@y.z&utm_source=news&token=1"))).toBe(
      "/docs?utm_source=news",
    );
    expect(pageUrl(new URL("https://a.example/docs?q=1#sec"))).toBe("/docs");
  });
});

describe("cleanReferrer", () => {
  it("remove query string e fragmento", () => {
    expect(cleanReferrer("https://ref.example/p?q=1#x", "a.example")).toBe("https://ref.example/p");
  });

  it("ignora o próprio site e valores inválidos", () => {
    expect(cleanReferrer("https://a.example/outra", "a.example")).toBe("");
    expect(cleanReferrer("não é url", "a.example")).toBe("");
    expect(cleanReferrer("", "a.example")).toBe("");
  });
});

describe("start", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  let stop = () => {};

  beforeEach(() => {
    vi.useFakeTimers();
    fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    history.replaceState(null, "", "/inicio?utm_campaign=lancamento&email=x@y.z");
    document.title = "Início";
  });

  afterEach(() => {
    stop();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("envia o page view ao gateway de onde o script veio", () => {
    stop = start(script({ src: "https://in.example/s/v1.0.0/n.js", "data-site": SITE }));

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://in.example/e");
    expect(init).toMatchObject({ method: "POST", credentials: "omit", keepalive: true });
    expect(sent(fetchMock)[0]).toEqual({
      type: "event",
      payload: {
        website: SITE,
        hostname: location.hostname,
        url: "/inicio?utm_campaign=lancamento",
        referrer: "",
        title: "Início",
        language: navigator.language,
        screen: `${screen.width}x${screen.height}`,
      },
    });
  });

  it("não faz nada sem data-site válido", () => {
    start(script({ src: "https://in.example/s/v1.0.0/n.js" }));
    start(script({ src: "https://in.example/s/v1.0.0/n.js", "data-site": "abc" }));
    start(null);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("conta mudanças de rota em SPA, uma vez por rota", () => {
    stop = start(script({ src: "https://in.example/s/v1.0.0/n.js", "data-site": SITE }));

    history.pushState(null, "", "/docs");
    document.title = "Docs";
    vi.advanceTimersByTime(NAVIGATION_DELAY_MS);
    // replaceState na mesma rota (ex.: só o estado mudou) não conta.
    history.replaceState({ aba: 2 }, "", "/docs");
    vi.advanceTimersByTime(NAVIGATION_DELAY_MS);
    // Voltar no navegador muda a URL sem passar por pushState e dispara popstate.
    History.prototype.replaceState.call(history, null, "", "/inicio");
    dispatchEvent(new PopStateEvent("popstate"));
    vi.advanceTimersByTime(NAVIGATION_DELAY_MS);

    const events = sent(fetchMock).map((e) => e.payload);
    expect(events.map((e) => e.url)).toEqual(["/inicio?utm_campaign=lancamento", "/docs", "/inicio"]);
    expect(events[1]).toMatchObject({ title: "Docs", referrer: `${location.origin}/inicio` });
    expect(events[2]).toMatchObject({ referrer: `${location.origin}/docs` });
  });

  it("não grava cookie nem storage", () => {
    const cookie = vi.spyOn(document, "cookie", "set");
    const setItem = vi.spyOn(Storage.prototype, "setItem");

    stop = start(script({ src: "https://in.example/s/v1.0.0/n.js", "data-site": SITE }));
    history.pushState(null, "", "/outra");
    vi.advanceTimersByTime(NAVIGATION_DELAY_MS);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(cookie).not.toHaveBeenCalled();
    expect(setItem).not.toHaveBeenCalled();
    expect(document.cookie).toBe("");
  });
});
