import "server-only";
import type { OperatorContext } from "../../access";
import { db } from "../../db";
import { site } from "../../db/schema";

// Funções do operador SCF (spec do E2, seção 4): recebem OperatorContext e são
// somente leitura, com exceção de remover e reativar tenant (#20).

// Vínculos portal → Umami de todos os tenants, inclusive sites arquivados.
// Usado pela checagem de consistência com o Umami.
export async function listUmamiLinks(
  ctx: OperatorContext,
): Promise<{ siteId: string; umamiWebsiteId: string }[]> {
  // O contexto não filtra nada aqui: exigi-lo prova que quem chama é operador.
  void ctx;
  return db.select({ siteId: site.id, umamiWebsiteId: site.umamiWebsiteId }).from(site);
}
