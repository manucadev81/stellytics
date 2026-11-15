import "server-only";
import { and, asc, count, desc, eq, isNull, like, sql } from "drizzle-orm";
import type { OperatorContext } from "../../access";
import { db } from "../../db";
import { apiService, auditLog, ingestKey, site, tenant, tenantMember, user } from "../../db/schema";

// Visão global do operador SCF (#19, spec do E2, seção 2): leitura de todos os
// tenants. Das chaves de ingestão, só prefixo e datas; nunca o hash.
//
// A única escrita é mudar o status do tenant (#20, #49): remover, reativar,
// aprovar e rejeitar.

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
  const [found] = await db
    .select({
      id: tenant.id,
      name: tenant.name,
      slug: tenant.slug,
      status: tenant.status,
      statusChangedAt: tenant.statusChangedAt,
      statusChangedBy: user.email,
      statusReason: tenant.statusReason,
      createdAt: tenant.createdAt,
    })
    .from(tenant)
    .leftJoin(user, eq(user.id, tenant.statusChangedBy))
    .where(eq(tenant.id, tenantId));
  if (!found) return null;

  const [memberRows, siteRows, serviceRows, keyRows, historyRows] = await Promise.all([
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
    // Histórico de status (criação, remoções e reativações), do audit_log.
    db
      .select({ at: auditLog.at, action: auditLog.action, actor: user.email, details: auditLog.details })
      .from(auditLog)
      .leftJoin(user, eq(user.id, auditLog.actorUserId))
      .where(and(eq(auditLog.tenantId, tenantId), like(auditLog.action, "tenant.%")))
      .orderBy(desc(auditLog.at)),
  ]);

  return {
    tenant: found,
    statusHistory: historyRows.map((h) => ({
      at: h.at,
      action: h.action,
      actor: h.actor,
      reason: (h.details as { reason?: string }).reason ?? null,
    })),
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

// Remoção e reativação de tenant (#20, spec do E2, seção 6, D6).
//
// Remover é desativar: os dados ficam no banco. As visões de ingestão
// (migração 0002) só aceitam tenant `active`, então o gateway web e o coletor
// passam a recusar a ingestão em até 1 minuto (o cache deles). Cada mudança
// vai para o audit_log com quem fez e o motivo.

export const REASON_MAX = 500;

export class TenantStatusError extends Error {
  constructor(
    readonly code: "invalid_reason",
    message: string,
  ) {
    super(message);
    this.name = "TenantStatusError";
  }
}

// Nulo: o tenant não existe. "unchanged": já estava no status pedido (por
// exemplo, dois operadores ao mesmo tempo); nada é gravado.
export type StatusChangeResult = "changed" | "unchanged" | null;

function cleanReason(raw: string | undefined, required: boolean): string | null {
  const reason = (raw ?? "").trim();
  if (reason.length > REASON_MAX) {
    throw new TenantStatusError("invalid_reason", `The reason must have at most ${REASON_MAX} characters.`);
  }
  if (required && reason.length < 3) {
    throw new TenantStatusError("invalid_reason", "Tell the project why.");
  }
  return reason || null;
}

type Transition = {
  to: "active" | "removed";
  // Status de onde a mudança vale; de outro, nada é gravado ("unchanged").
  from: ReadonlyArray<"active" | "removed" | "pending">;
  action: "tenant.removed" | "tenant.reactivated" | "tenant.approved" | "tenant.rejected";
};

async function changeStatus(
  ctx: OperatorContext,
  tenantId: string,
  transition: Transition,
  reason: string | null,
): Promise<StatusChangeResult> {
  if (!UUID_REGEX.test(tenantId)) return null;
  const { to, from, action } = transition;
  return db.transaction(async (tx) => {
    // Trava a linha: duas mudanças ao mesmo tempo gravam um registro só.
    const [current] = await tx
      .select({ status: tenant.status })
      .from(tenant)
      .where(eq(tenant.id, tenantId))
      .for("update");
    if (!current) return null;
    if (!from.includes(current.status)) return "unchanged";

    await tx
      .update(tenant)
      .set({
        status: to,
        statusChangedAt: sql`clock_timestamp()`,
        statusChangedBy: ctx.userId,
        // O motivo da coluna é o do status atual; o da reativação e o da
        // aprovação ficam só no audit_log.
        statusReason: to === "removed" ? reason : null,
      })
      .where(eq(tenant.id, tenantId));
    await tx.insert(auditLog).values({
      // Horário de agora, já com a trava: o now() padrão é o do início da
      // transação, e mudanças concorrentes sairiam fora de ordem no histórico.
      at: sql`clock_timestamp()`,
      actorUserId: ctx.userId,
      action,
      tenantId,
      details: { from: current.status, ...(reason ? { reason } : {}) },
    });
    return "changed";
  });
}

// O motivo é obrigatório: o projeto o vê no painel.
export async function removeTenant(
  ctx: OperatorContext,
  tenantId: string,
  input: { reason: string },
): Promise<StatusChangeResult> {
  return changeStatus(
    ctx,
    tenantId,
    { to: "removed", from: ["active", "pending"], action: "tenant.removed" },
    cleanReason(input.reason, true),
  );
}

// O motivo é opcional e fica só no audit_log; o painel do projeto volta ao normal.
export async function reactivateTenant(
  ctx: OperatorContext,
  tenantId: string,
  input: { reason?: string } = {},
): Promise<StatusChangeResult> {
  // Reativar vale só para tenant removido; `pending` se aprova em approveTenant (#49).
  return changeStatus(
    ctx,
    tenantId,
    { to: "active", from: ["removed"], action: "tenant.reactivated" },
    cleanReason(input.reason, false),
  );
}

// Aprovação de cadastro (#49, ADR 0007). Só vale para tenant `pending`; de
// outro status devolve "unchanged". Aprovar vira `active` (a ingestão passa a
// aceitar em até 1 minuto, pelas visões). Rejeitar vira `removed`, com o motivo
// que o projeto vê no painel; o operador ainda pode reativar depois (D6).
// O motivo da aprovação é opcional e fica só no audit_log.
export async function approveTenant(
  ctx: OperatorContext,
  tenantId: string,
  input: { reason?: string } = {},
): Promise<StatusChangeResult> {
  return changeStatus(
    ctx,
    tenantId,
    { to: "active", from: ["pending"], action: "tenant.approved" },
    cleanReason(input.reason, false),
  );
}

export async function rejectTenant(
  ctx: OperatorContext,
  tenantId: string,
  input: { reason: string },
): Promise<StatusChangeResult> {
  return changeStatus(
    ctx,
    tenantId,
    { to: "removed", from: ["pending"], action: "tenant.rejected" },
    cleanReason(input.reason, true),
  );
}

// Projetos à espera de aprovação, do mais antigo para o mais novo, com quem
// os criou. Só nome, e-mail e data: nada além do que o detalhe já mostra.
export async function listPendingTenants(ctx: OperatorContext) {
  void ctx;
  return db
    .select({
      id: tenant.id,
      name: tenant.name,
      slug: tenant.slug,
      createdAt: tenant.createdAt,
      ownerEmail: user.email,
    })
    .from(tenant)
    .leftJoin(tenantMember, eq(tenantMember.tenantId, tenant.id))
    .leftJoin(user, eq(user.id, tenantMember.userId))
    .where(eq(tenant.status, "pending"))
    .orderBy(asc(tenant.createdAt));
}
