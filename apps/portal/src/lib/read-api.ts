import "server-only";
import { authenticateReadKey, type OperatorKeyContext } from "./access";
import { parsePeriodKey, PERIODS, type Period, resolvePeriod } from "./period";
import { takeToken } from "./rate-limit";

// Base das rotas da API de leitura do SCF (#48, ADR 0006): autenticação por
// chave, limite por chave e respostas JSON sem cache. Erros não dizem por que
// a chave foi recusada.

const HEADERS = { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" };

export function json(body: unknown, init: { status?: number; headers?: Record<string, string> } = {}): Response {
  return Response.json(body, { status: init.status ?? 200, headers: { ...HEADERS, ...init.headers } });
}

export function error(status: number, code: string, message: string, headers: Record<string, string> = {}) {
  return json({ error: { code, message } }, { status, headers });
}

// `?period=` aceita as chaves do painel (24h, 7d, 30d, 90d, 12m); sem o
// parâmetro vale o padrão do painel. Valor desconhecido é erro, não padrão.
function periodOf(url: URL): Period | null {
  const raw = url.searchParams.get("period");
  if (raw === null) return resolvePeriod(parsePeriodKey(undefined));
  return Object.hasOwn(PERIODS, raw) ? resolvePeriod(parsePeriodKey(raw)) : null;
}

export async function handleRead(
  request: Request,
  handler: (ctx: OperatorKeyContext, period: Period) => Promise<unknown>,
): Promise<Response> {
  const ctx = await authenticateReadKey(request.headers.get("authorization"));
  if (!ctx) {
    return error(401, "unauthorized", "Missing or invalid API key.", { "WWW-Authenticate": 'Bearer realm="notify"' });
  }
  const rate = takeToken(ctx.keyId);
  if (!rate.ok) {
    return error(429, "rate_limited", "Too many requests. Try again later.", {
      "Retry-After": String(rate.retryAfterSeconds),
    });
  }
  const period = periodOf(new URL(request.url));
  if (!period) {
    return error(400, "invalid_period", `period must be one of: ${Object.keys(PERIODS).join(", ")}.`);
  }
  try {
    return json(await handler(ctx, period));
  } catch (e) {
    // Só o nome do erro: o log não leva chave, URL nem detalhe do Umami.
    console.error("Falha na API de leitura", (e as Error).name);
    return error(500, "internal_error", "Something went wrong.");
  }
}

export function periodBody(period: Period) {
  return { key: period.key, startAt: period.startAt.toISOString(), endAt: period.endAt.toISOString() };
}
