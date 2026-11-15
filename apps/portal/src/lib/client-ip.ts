import { isIP } from "node:net";

// IP truncado do cliente, só para o limite de requisições em memória da API de
// leitura (#48). Nunca é gravado nem logado.
//
// Em produção o portal só é alcançado pelo Caddy, que não tem trusted_proxies:
// ele descarta o X-Forwarded-For que chega e escreve o IP da conexão. Por isso
// vale a ÚLTIMA entrada do cabeçalho (a que o Caddy acrescentou), nunca a
// primeira, que o cliente controla. Se o portal ficasse exposto sem o Caddy, o
// cabeçalho seria forjável e o limite por IP não valeria. Sem cabeçalho
// (desenvolvimento local), todos caem no mesmo balde.

export function truncatedClientIp(forwardedFor: string | null): string {
  const last = forwardedFor?.split(",").at(-1)?.trim() ?? "";
  const family = isIP(last);
  if (family === 4) return `${last.split(".").slice(0, 3).join(".")}.0`;
  if (family === 6) {
    // /48: os três primeiros grupos, depois de expandir o "::".
    const [head, tail = ""] = last.split("::");
    const h = head ? head.split(":") : [];
    const t = tail ? tail.split(":") : [];
    const groups = [...h, ...Array(Math.max(0, 8 - h.length - t.length)).fill("0"), ...t];
    return `${groups.slice(0, 3).join(":")}::/48`;
  }
  return "unknown";
}
