import "server-only";
import type { OperatorContext } from "../access";
import { listUmamiLinks } from "../data/operator/sites";
import { umamiRequest } from "./http";

// Teste de consistência do spec do E2, seção 7: todo `site.umami_website_id`
// existe no Umami, e todo site do Umami tem linha em `site`. Checagem global,
// só para o operador; devolve apenas IDs.

type WebsitePage = { data: { id: string }[]; count: number };

const PAGE_SIZE = 100;

async function listUmamiWebsiteIds(): Promise<Set<string>> {
  const ids = new Set<string>();
  for (let page = 1; ; page++) {
    const { data, count } = await umamiRequest<WebsitePage>(
      `/api/websites?page=${page}&pageSize=${PAGE_SIZE}`,
    );
    for (const website of data) ids.add(website.id);
    if (data.length === 0 || page * PAGE_SIZE >= count) return ids;
  }
}

export async function findUmamiInconsistencies(ctx: OperatorContext) {
  const inUmami = await listUmamiWebsiteIds();
  const links = await listUmamiLinks(ctx);
  const inPortal = new Set(links.map((l) => l.umamiWebsiteId));
  return {
    // IDs de `site` cujo site sumiu do Umami.
    missingInUmami: links.filter((l) => !inUmami.has(l.umamiWebsiteId)).map((l) => l.siteId),
    // Sites do Umami sem linha em `site` (por exemplo, um rollback que falhou).
    orphanedInUmami: [...inUmami].filter((id) => !inPortal.has(id)),
  };
}
