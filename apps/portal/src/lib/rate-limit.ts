// Limite de requisições da API de leitura (#48, ADR 0006): janela fixa de um
// minuto por chave, em memória. Com mais de uma instância do portal o limite
// vale por instância; para o uso previsto (poucas chaves do SCF) isso basta.

export const READ_API_LIMIT = 60;
const WINDOW_MS = 60_000;

type Entry = { windowStart: number; used: number };
const entries = new Map<string, Entry>();

export type RateResult = { ok: true } | { ok: false; retryAfterSeconds: number };

export function takeToken(key: string, now: number = Date.now(), limit: number = READ_API_LIMIT): RateResult {
  // Descarta janelas vencidas quando o mapa cresce (há uma entrada por chave e
  // por IP truncado), para a memória não aumentar sem limite.
  if (entries.size > 10_000) {
    for (const [k, e] of entries) if (now - e.windowStart >= WINDOW_MS) entries.delete(k);
  }
  const current = entries.get(key);
  if (!current || now - current.windowStart >= WINDOW_MS) {
    entries.set(key, { windowStart: now, used: 1 });
    return { ok: true };
  }
  if (current.used >= limit) {
    return { ok: false, retryAfterSeconds: Math.max(1, Math.ceil((current.windowStart + WINDOW_MS - now) / 1000)) };
  }
  current.used++;
  return { ok: true };
}

export function resetRateLimits() {
  entries.clear();
}
