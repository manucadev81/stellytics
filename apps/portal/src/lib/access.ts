import "server-only";
import { eq } from "drizzle-orm";
import { notFound } from "next/navigation";
import { db } from "./db";
import { tenant, tenantMember } from "./db/schema";
import { requireSession } from "./session";

// Contextos de acesso do spec do E2, seção 4 (ADR 0003). O tenant vem sempre
// da sessão, nunca de um valor enviado pelo cliente.
//
// Os tipos são marcados (branded): fora deste arquivo ninguém consegue montar
// um contexto à mão, então as funções de src/lib/data/ e src/lib/umami/ só
// recebem contextos criados aqui, a partir da sessão. Junto com a regra de
// lint (eslint.config.mjs), isso faz de data/ e umami/ a camada única.
//
// Quem não tem acesso recebe 404, nunca 403, para não revelar que o recurso
// ou a área existe.

declare const tenantContextBrand: unique symbol;
declare const operatorContextBrand: unique symbol;

export type TenantContext = {
  readonly userId: string;
  readonly tenantId: string;
  readonly memberRole: "owner" | "member";
  readonly tenantStatus: "active" | "removed" | "pending";
  readonly [tenantContextBrand]: true;
};

export type OperatorContext = {
  readonly userId: string;
  readonly [operatorContextBrand]: true;
};

// Busca o tenant do usuário (D1: no máximo um). Nulo para quem não é membro.
// Não confere o papel: use getTenantContext() ou requireTenantContext().
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

// Contexto do tenant de quem está na sessão, ou nulo para quem ainda não tem
// projeto. Sem sessão vai para o login; operador recebe 404 (D2: operador não
// é membro de tenant).
export async function getTenantContext(): Promise<TenantContext | null> {
  const session = await requireSession();
  if (session.user.role === "operator") notFound();
  return loadTenantContext(session.user.id);
}

export async function requireTenantContext(): Promise<TenantContext> {
  const ctx = await getTenantContext();
  if (!ctx) notFound();
  return ctx;
}

export async function requireOperatorContext(): Promise<OperatorContext> {
  const session = await requireSession();
  if (session.user.role !== "operator") notFound();
  return { userId: session.user.id } as OperatorContext;
}
