import "server-only";
import { and, asc, eq, isNull } from "drizzle-orm";
import type { TenantContext } from "../access";
import { db } from "../db";
import { site } from "../db/schema";
import { provisionUmamiWebsite } from "../umami/provision";

// Sites do tenant. Toda função recebe o contexto e filtra por ctx.tenantId
// (spec do E2, seção 4).

export type Site = typeof site.$inferSelect;

export class SiteInputError extends Error {
  constructor(
    readonly field: "name" | "domain" | "tenant",
    message: string,
  ) {
    super(message);
    this.name = "SiteInputError";
  }
}

// Mesma regra do Umami 3.4.1 (DOMAIN_REGEX em src/lib/constants.ts), para
// recusar aqui o que o Umami recusaria.
const DOMAIN_REGEX =
  /^(localhost(:[1-9]\d{0,4})?|((?=[a-z0-9-_]{1,63}\.)(xn--)?[a-z0-9-_]+(-[a-z0-9-_]+)*\.)+(xn--)?[a-z0-9-_]{2,63})$/;

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Aceita "https://Example.org/path" e grava "example.org".
export function normalizeDomain(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^[a-z][a-z0-9+.-]*:\/\//, "")
    .replace(/[/?#].*$/, "")
    .replace(/\.$/, "");
}

export async function createSite(
  ctx: TenantContext,
  input: { name: string; domain: string },
): Promise<Site> {
  // Spec do E2, seção 6: tenant removido não cadastra site.
  if (ctx.tenantStatus === "removed") {
    throw new SiteInputError("tenant", "This project was removed.");
  }
  const name = input.name.trim();
  if (name.length < 1 || name.length > 100) {
    throw new SiteInputError("name", "Name must have between 1 and 100 characters.");
  }
  const domain = normalizeDomain(input.domain);
  if (domain.length > 500 || !DOMAIN_REGEX.test(domain)) {
    throw new SiteInputError("domain", "Enter a valid domain, like example.org.");
  }

  // Confere antes para não criar um site no Umami à toa. O índice único
  // (tenant_id, domain) continua valendo numa corrida.
  const [duplicate] = await db
    .select({ id: site.id })
    .from(site)
    .where(and(eq(site.tenantId, ctx.tenantId), eq(site.domain, domain)))
    .limit(1);
  if (duplicate) {
    throw new SiteInputError("domain", "This domain is already registered in your project.");
  }

  return provisionUmamiWebsite({ name, domain }, async (umamiWebsiteId) => {
    const [created] = await db
      .insert(site)
      .values({ tenantId: ctx.tenantId, name, domain, umamiWebsiteId })
      .returning();
    return created!;
  });
}

// Nulo quando o site não existe, está arquivado ou é de outro tenant: quem
// chama responde 404 nos três casos.
export async function getSite(ctx: TenantContext, siteId: string): Promise<Site | null> {
  if (!UUID_REGEX.test(siteId)) return null;
  const [found] = await db
    .select()
    .from(site)
    .where(and(eq(site.id, siteId), eq(site.tenantId, ctx.tenantId), isNull(site.archivedAt)))
    .limit(1);
  return found ?? null;
}

export async function listSites(ctx: TenantContext): Promise<Site[]> {
  return db
    .select()
    .from(site)
    .where(and(eq(site.tenantId, ctx.tenantId), isNull(site.archivedAt)))
    .orderBy(asc(site.createdAt));
}
