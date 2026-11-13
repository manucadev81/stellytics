import "server-only";
import { randomBytes } from "node:crypto";
import { eq } from "drizzle-orm";
import type { OnboardingContext, TenantContext } from "../access";
import { db } from "../db";
import { auditLog, tenant, tenantMember } from "../db/schema";

// Criação do projeto no cadastro self-service (#17, spec do E2, seção 7):
// o tenant nasce `active` e quem o criou vira `owner`, na mesma transação.
// Quando o cadastro com aprovação existir (#49), o status passa a `pending`.

export class ProjectError extends Error {
  constructor(
    readonly code: "invalid_name" | "already_member",
    message: string,
  ) {
    super(message);
    this.name = "ProjectError";
  }
}

// "Soroban Explorer!" -> "soroban-explorer". Só ASCII, até 40 caracteres.
export function slugify(name: string): string {
  const slug = name
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "");
  return slug || "project";
}

function constraintOf(error: unknown): string | undefined {
  // O Drizzle embrulha o erro do Postgres em `cause`.
  return (error as { cause?: { constraint_name?: string } }).cause?.constraint_name;
}

export async function createProject(
  ctx: OnboardingContext,
  input: { name: string },
): Promise<{ id: string; name: string; slug: string }> {
  const name = input.name.trim();
  if (name.length < 2 || name.length > 100) {
    throw new ProjectError("invalid_name", "Project name must have between 2 and 100 characters.");
  }
  const base = slugify(name);

  // Slug repetido: tenta de novo com um sufixo aleatório.
  for (let attempt = 0; attempt < 5; attempt++) {
    const slug = attempt === 0 ? base : `${base.slice(0, 33)}-${randomBytes(3).toString("hex")}`;
    try {
      return await db.transaction(async (tx) => {
        const [created] = await tx
          .insert(tenant)
          .values({ name, slug })
          .returning({ id: tenant.id, name: tenant.name, slug: tenant.slug });
        await tx.insert(tenantMember).values({ tenantId: created!.id, userId: ctx.userId, role: "owner" });
        await tx.insert(auditLog).values({
          actorUserId: ctx.userId,
          action: "tenant.created",
          tenantId: created!.id,
          details: { slug },
        });
        return created!;
      });
    } catch (error) {
      const constraint = constraintOf(error);
      if (constraint === "tenant_slug_unique") continue;
      // D1: dois pedidos ao mesmo tempo do mesmo usuário; vale o índice único.
      if (constraint === "tenant_member_user_id_key") {
        throw new ProjectError("already_member", "You already have a project.");
      }
      throw error;
    }
  }
  throw new Error("Could not find a free project slug");
}

// Projeto do tenant do contexto, para o painel.
export async function getProject(ctx: TenantContext) {
  const [found] = await db
    .select({ name: tenant.name, slug: tenant.slug, status: tenant.status, statusReason: tenant.statusReason })
    .from(tenant)
    .where(eq(tenant.id, ctx.tenantId));
  return found!;
}
