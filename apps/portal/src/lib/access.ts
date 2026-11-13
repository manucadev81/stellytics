import "server-only";
import { eq } from "drizzle-orm";
import { notFound } from "next/navigation";
import { db } from "./db";
import { tenant, tenantMember } from "./db/schema";
import { requireSession } from "./session";

// Contexto de acesso do spec do E2, seção 4. O tenant vem sempre da sessão,
// nunca de um valor enviado pelo cliente (ADR 0003). O tipo é marcado
// (branded): fora deste arquivo ninguém consegue montar um contexto à mão,
// então as funções de src/lib/data/ e src/lib/umami/ só recebem contextos
// criados aqui. A regra de lint e o contexto do operador completam isto na #18.
declare const tenantContextBrand: unique symbol;

export type TenantContext = {
  readonly userId: string;
  readonly tenantId: string;
  readonly memberRole: "owner" | "member";
  readonly tenantStatus: "active" | "removed" | "pending";
  readonly [tenantContextBrand]: true;
};

// Busca o tenant do usuário (D1: no máximo um). Nulo para quem não é membro.
export async function loadTenantContext(userId: string): Promise<TenantContext | null> {
  const [row] = await db
    .select({ tenantId: tenantMember.tenantId, memberRole: tenantMember.role, tenantStatus: tenant.status })
    .from(tenantMember)
    .innerJoin(tenant, eq(tenant.id, tenantMember.tenantId))
    .where(eq(tenantMember.userId, userId))
    .limit(1);
  if (!row) return null;
  return { userId, ...row } as TenantContext;
}

export async function requireTenantContext(): Promise<TenantContext> {
  const session = await requireSession();
  // D2: operador não é membro de tenant. 404 para não revelar a área.
  if (session.user.role === "operator") notFound();
  const ctx = await loadTenantContext(session.user.id);
  // Usuário sem projeto: a #17 troca isto pelo redirecionamento à criação de projeto.
  if (!ctx) notFound();
  return ctx;
}
