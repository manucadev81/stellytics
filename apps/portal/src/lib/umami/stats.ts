import "server-only";
import type { TenantContext } from "../access";
import { getSite } from "../data/sites";
import { umamiRequest } from "./http";

// Leitura de estatísticas web. Recebe o ID do site do portal, nunca o do
// Umami: o ID do Umami só é obtido depois de conferir que o site é do tenant
// do contexto (ADR 0003). Site de outro tenant devolve nulo sem chamar o Umami.

type Totals = {
  pageviews: number;
  visitors: number;
  visits: number;
  bounces: number;
  totaltime: number;
};

export type SiteStats = Totals & { comparison: Totals };

export async function getSiteStats(
  ctx: TenantContext,
  siteId: string,
  range: { startAt: Date; endAt: Date },
): Promise<SiteStats | null> {
  const found = await getSite(ctx, siteId);
  if (!found) return null;
  const query = new URLSearchParams({
    startAt: String(range.startAt.getTime()),
    endAt: String(range.endAt.getTime()),
  });
  return umamiRequest<SiteStats>(`/api/websites/${found.umamiWebsiteId}/stats?${query}`);
}
