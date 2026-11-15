import { listActiveProjects } from "@/lib/data/operator/projects";
import { handleRead } from "@/lib/read-api";

export const dynamic = "force-dynamic";

// GET /api/v1/projects: projetos ativos (id, nome, slug e contagens).
export function GET(request: Request) {
  return handleRead(request, async (ctx) => ({ projects: await listActiveProjects(ctx) }));
}
