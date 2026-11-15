import { getApiOverview } from "@/lib/data/operator/api";
import { handleRead, periodBody } from "@/lib/read-api";

export const dynamic = "force-dynamic";

// GET /api/v1/api?period=7d: métricas de API de cada projeto ativo, as mesmas
// do painel /operator/api. Latências em segundos (p50, p95), nulas sem requisições.
export function GET(request: Request) {
  return handleRead(request, async (ctx, period) => ({
    period: periodBody(period),
    projects: (await getApiOverview(ctx, period)).map((p) => ({
      id: p.tenantId,
      name: p.name,
      slug: p.slug,
      apiServices: p.services,
      totals: p.totals,
    })),
  }));
}
