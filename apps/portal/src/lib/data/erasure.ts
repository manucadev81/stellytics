import "server-only";
import { eq, sql } from "drizzle-orm";
import { db } from "../db";
import { apiService, auditLog, ingestKey, operatorApiKey, site, tenant, tenantMember, user } from "../db/schema";
import { deleteUmamiWebsite } from "../umami/purge";

// Direito ao apagamento (#154): apagar os dados de um tenant e excluir a
// própria conta. Decisões registradas em docs/privacidade.md (seções 6 e 7):
//
// - A linha do tenant não sai: o histórico do audit_log dela continua
//   legível e o slug não é reutilizado. O que sai são os dados coletados.
// - As ações do usuário no audit_log são anonimizadas, não apagadas: os
//   gatilhos (0002 e 0009) aceitam trocar `actor_user_id` pelo marcador e
//   nada mais.
// - Os sites saem do Umami ANTES do banco: se o apagamento no banco falhar,
//   repetir completa; ao contrário, um site apagado aqui mas vivo no Umami
//   seguiria coletando eventos sem dono. O DELETE no Umami trata 404 como
//   sucesso, então repetir nunca trava no meio.

// Mesmo valor do gatilho em 0009_erasure.sql; o teste de isolamento confere
// os dois lados.
export const ANONYMIZED_ACTOR = "[apagado]";

export class ErasureError extends Error {
  constructor(
    readonly code: "tenant_active",
    message: string,
  ) {
    super(message);
    this.name = "ErasureError";
  }
}

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export type PurgeResult = { sites: number };

// Apaga os dados coletados de um tenant: sites no Umami (arquivados
// inclusive), linhas de site, serviço e chave e as métricas de API nas três
// tabelas (api_metric_purge_tenant, migração 0009). Quando o tenant ainda
// não está `removed`, marca como removido com o motivo dado. Chamado pelo
// operador (purgeRemovedTenant) e pela exclusão da própria conta.
export async function purgeTenantData(input: {
  tenantId: string;
  actorUserId: string | null;
  statusReason: string;
}): Promise<PurgeResult> {
  if (!UUID_REGEX.test(input.tenantId)) throw new ErasureError("tenant_active", "This project was not found.");
  const [current] = await db
    .select({ status: tenant.status })
    .from(tenant)
    .where(eq(tenant.id, input.tenantId))
    .limit(1);
  if (!current) throw new ErasureError("tenant_active", "This project was not found.");

  const siteRows = await db
    .select({ umamiWebsiteId: site.umamiWebsiteId })
    .from(site)
    .where(eq(site.tenantId, input.tenantId));
  for (const row of siteRows) await deleteUmamiWebsite(row.umamiWebsiteId);

  await db.transaction(async (tx) => {
    // A chave referencia o serviço (ingest_key_api_service_fk): sai primeiro.
    await tx.delete(ingestKey).where(eq(ingestKey.tenantId, input.tenantId));
    await tx.delete(apiService).where(eq(apiService.tenantId, input.tenantId));
    await tx.delete(site).where(eq(site.tenantId, input.tenantId));
    await tx.execute(sql`select api_metric_purge_tenant(${input.tenantId}::uuid)`);
    if (current.status !== "removed") {
      await tx
        .update(tenant)
        .set({
          status: "removed",
          // Horário de agora, como em changeStatus (data/operator/tenants.ts):
          // mudanças concorrentes ficam em ordem no histórico.
          statusChangedAt: sql`clock_timestamp()`,
          statusChangedBy: input.actorUserId,
          statusReason: input.statusReason,
        })
        .where(eq(tenant.id, input.tenantId));
    }
    await tx.insert(auditLog).values({
      at: sql`clock_timestamp()`,
      actorUserId: input.actorUserId,
      action: "tenant.purged",
      tenantId: input.tenantId,
      details: { sites: siteRows.length },
    });
  });
  return { sites: siteRows.length };
}

// Exclui a própria conta. Nulo quando o usuário não existe (já excluído, por
// exemplo num clique duplo). Fluxo:
//
// 1. Membro e dono do projeto (D1: todo usuário pertence a um tenant): apaga
//    os dados do tenant antes da conta. Só o dono chega aqui com projeto para
//    apagar; se um dia houver convites (#D1 retirando o índice único), esta
//    regra precisa de revisão.
// 2. Num banco, na ordem: chaves de leitura do SCF criadas pelo usuário
//    (chave estrangeira restrict), o registro final `account.deleted`, a
//    anonimização de todas as ações dele no audit_log (a própria exclusão
//    fica com o marcador) e a linha do usuário — session, account e
//    tenant_member saem em cascata, e tenant.status_changed_by vira nulo.
export async function deleteOwnAccount(userId: string): Promise<PurgeResult | null> {
  const [account] = await db.select({ id: user.id }).from(user).where(eq(user.id, userId)).limit(1);
  if (!account) return null;
  const [membership] = await db
    .select({ tenantId: tenantMember.tenantId, role: tenantMember.role })
    .from(tenantMember)
    .where(eq(tenantMember.userId, userId))
    .limit(1);

  let purged: PurgeResult = { sites: 0 };
  if (membership && membership.role === "owner") {
    purged = await purgeTenantData({
      tenantId: membership.tenantId,
      actorUserId: userId,
      statusReason: "Account deleted.",
    });
  }

  await db.transaction(async (tx) => {
    await tx.delete(operatorApiKey).where(eq(operatorApiKey.createdBy, userId));
    await tx.insert(auditLog).values({
      at: sql`clock_timestamp()`,
      actorUserId: userId,
      action: "account.deleted",
      tenantId: membership?.tenantId ?? null,
      details: purged,
    });
    await tx
      .update(auditLog)
      .set({ actorUserId: ANONYMIZED_ACTOR })
      .where(eq(auditLog.actorUserId, userId));
    await tx.delete(user).where(eq(user.id, userId));
  });
  return purged;
}
