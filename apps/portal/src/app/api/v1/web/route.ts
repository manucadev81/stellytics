import { getWebOverview } from "@/lib/umami/overview";
import { handleRead, periodBody } from "@/lib/read-api";

export const dynamic = "force-dynamic";

// GET /api/v1/web?period=7d: métricas web de cada projeto ativo, as mesmas do
// painel /operator/web. `totals` e `previous` são nulos quando o Umami não
// respondeu para algum site do projeto.
export function GET(request: Request) {
  return handleRead(request, async (ctx, period) => {
    const { projects, unavailable } = await getWebOverview(ctx, period);
    return {
      period: periodBody(period),
      unavailable,
      projects: projects.map((p) => ({
        id: p.tenantId,
        name: p.name,
        slug: p.slug,
        websites: p.sites,
        totals: p.totals,
        previous: p.previous,
      })),
    };
  });
}
