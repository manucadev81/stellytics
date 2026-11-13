import "server-only";
import { asc, count, desc, eq, isNull, sql } from "drizzle-orm";
import type { OperatorContext } from "../../access";
import { db } from "../../db";
import { apiService, ingestKey, site, tenant, tenantMember, user } from "../../db/schema";

// Visão global do operador SCF (#19, spec do E2, seção 2): leitura de todos os
// tenants. Das chaves de ingestão, só prefixo e datas; nunca o hash.

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export type TenantOverview = {
  id: string;
  name: string;
  slug: string;
  status: "active" | "removed" | "pending";
  createdAt: Date;
  members: number;
  sites: number;
  services: number;
  activeKeys: number;
};

export async function listTenants(ctx: OperatorContext): Promise<TenantOverview[]> {
  void ctx; // Exigir o contexto prova que quem chama é operador.
  const members = db
    .select({ tenantId: tenantMember.tenantId, n: count().as("members") })
    .from(tenantMember)
    .groupBy(tenantMember.tenantId)
    .as("members");
  const sites = db
    .select({ tenantId: site.tenantId, n: count().as("sites") })
    .from(site)
    .where(isNull(site.archivedAt))
    .groupBy(site.tenantId)
    .as("sites");
  const services = db
    .select({ tenantId: apiService.tenantId, n: count().as("services") })
    .from(apiService)
    .where(isNull(apiService.archivedAt))
    .groupBy(apiService.tenantId)
    .as("services");
  const keys = db
    .select({ tenantId: ingestKey.tenantId, n: count().as("keys") })
    .from(ingestKey)
    .where(isNull(ingestKey.revokedAt))
    .groupBy(ingestKey.tenantId)
    .as("keys");

  return db
    .select({
      id: tenant.id,
      name: tenant.name,
      slug: tenant.slug,
      status: tenant.status,
      createdAt: tenant.createdAt,
      members: sql<number>`coalesce(${members.n}, 0)::int`,
      sites: sql<number>`coalesce(${sites.n}, 0)::int`,
      services: sql<number>`coalesce(${services.n}, 0)::int`,
      activeKeys: sql<number>`coalesce(${keys.n}, 0)::int`,
    })
    .from(tenant)
    .leftJoin(members, eq(members.tenantId, tenant.id))
    .leftJoin(sites, eq(sites.tenantId, tenant.id))
    .leftJoin(services, eq(services.tenantId, tenant.id))
    .leftJoin(keys, eq(keys.tenantId, tenant.id))
    .orderBy(asc(tenant.name));
}

// Detalhe de um tenant, inclusive removido. Nulo quando não existe.
export async function getTenantDetail(ctx: OperatorContext, tenantId: string) {
  void ctx;
  if (!UUID_REGEX.test(tenantId)) return null;
  const [found] = await db.select().from(tenant).where(eq(tenant.id, tenantId));
  if (!found) return null;

  const [memberRows, siteRows, serviceRows, keyRows] = await Promise.all([
    db
      .select({ name: user.name, email: user.email, role: tenantMember.role, since: tenantMember.createdAt })
      .from(tenantMember)
      .innerJoin(user, eq(user.id, tenantMember.userId))
      .where(eq(tenantMember.tenantId, tenantId))
      .orderBy(asc(tenantMember.createdAt)),
    db
      .select({
        id: site.id,
        name: site.name,
        domain: site.domain,
        umamiWebsiteId: site.umamiWebsiteId,
        createdAt: site.createdAt,
        archivedAt: site.archivedAt,
      })
      .from(site)
      .where(eq(site.tenantId, tenantId))
      .orderBy(asc(site.createdAt)),
    db
      .select({ id: apiService.id, name: apiService.name, archivedAt: apiService.archivedAt })
      .from(apiService)
      .where(eq(apiService.tenantId, tenantId))
      .orderBy(asc(apiService.name)),
    db
      .select({
        apiServiceId: ingestKey.apiServiceId,
        prefix: ingestKey.prefix,
        createdAt: ingestKey.createdAt,
        revokedAt: ingestKey.revokedAt,
        lastUsedOn: ingestKey.lastUsedOn,
      })
      .from(ingestKey)
      .where(eq(ingestKey.tenantId, tenantId))
      .orderBy(desc(ingestKey.createdAt)),
  ]);

  return {
    tenant: {
      id: found.id,
      name: found.name,
      slug: found.slug,
      status: found.status,
      statusChangedAt: found.statusChangedAt,
      statusReason: found.statusReason,
      createdAt: found.createdAt,
    },
    members: memberRows,
    sites: siteRows,
    services: serviceRows.map((service) => ({
      ...service,
      keys: keyRows
        .filter((k) => k.apiServiceId === service.id)
        .map((k) => ({ prefix: k.prefix, createdAt: k.createdAt, revokedAt: k.revokedAt, lastUsedOn: k.lastUsedOn })),
    })),
  };
}
