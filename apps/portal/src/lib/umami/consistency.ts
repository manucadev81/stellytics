import "server-only";
import { db } from "../db";
import { site } from "../db/schema";
import { umamiRequest } from "./http";

// Teste de consistência do spec do E2, seção 7: todo `site.umami_website_id`
// existe no Umami, e todo site do Umami tem linha em `site`. É uma checagem
// global, para o operador e a monitoração; não devolve dados de nenhum tenant
// além dos IDs. Ganha um OperatorContext na #19.

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

export async function findUmamiInconsistencies() {
  const inUmami = await listUmamiWebsiteIds();
  const rows = await db.select({ id: site.id, umamiWebsiteId: site.umamiWebsiteId }).from(site);
  const inPortal = new Set(rows.map((r) => r.umamiWebsiteId));
  return {
    // IDs de `site` cujo site sumiu do Umami.
    missingInUmami: rows.filter((r) => !inUmami.has(r.umamiWebsiteId)).map((r) => r.id),
    // Sites do Umami sem linha em `site` (por exemplo, um rollback que falhou).
    orphanedInUmami: [...inUmami].filter((id) => !inPortal.has(id)),
  };
}
