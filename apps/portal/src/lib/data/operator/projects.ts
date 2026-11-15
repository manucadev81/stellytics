import "server-only";
import { sql } from "../../db";
import type { OperatorContext } from "../../access";

// Lista de projetos ativos para a API de leitura do SCF (#48). Mesmo critério
// dos painéis globais: projeto removido ou pendente não aparece.

export type ActiveProject = {
  id: string;
  name: string;
  slug: string;
  websites: number;
  apiServices: number;
};

export async function listActiveProjects(ctx: OperatorContext): Promise<ActiveProject[]> {
  void ctx;
  const rows = await sql<{ id: string; name: string; slug: string; websites: string; api_services: string }[]>`
    SELECT t.id, t.name, t.slug,
      (SELECT count(*) FROM site s WHERE s.tenant_id = t.id AND s.archived_at IS NULL) AS websites,
      (SELECT count(*) FROM api_service a WHERE a.tenant_id = t.id AND a.archived_at IS NULL) AS api_services
    FROM tenant t
    WHERE t.status = 'active'
    ORDER BY t.name, t.id`;
  return rows.map((r) => ({
    id: r.id,
    name: r.name,
    slug: r.slug,
    websites: Number(r.websites),
    apiServices: Number(r.api_services),
  }));
}
