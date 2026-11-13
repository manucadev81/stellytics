// Contextos de acesso (#18): o tenant vem da sessão e quem não tem acesso
// recebe 404. A sessão é simulada; o resto roda contra o Postgres de DATABASE_URL.
import { randomUUID } from "node:crypto";
import { inArray, like } from "drizzle-orm";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getTenantContext,
  requireOperatorContext,
  requireTenantContext,
} from "@/lib/access";
import { db, sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { tenant, tenantMember, user } from "@/lib/db/schema";

const DOMAIN = "@access-test.notify.invalid";
const SLUG = "access-test-";

// Usuário da sessão simulada; null simula quem não entrou.
let sessionUser: { id: string; role: "tenant" | "operator" } | null = null;

vi.mock("@/lib/session", () => ({
  requireSession: async () => {
    if (!sessionUser) throw Object.assign(new Error("NEXT_REDIRECT"), { digest: "NEXT_REDIRECT;/sign-in" });
    return { user: { ...sessionUser, name: "Test", email: "test@example.invalid" } };
  },
}));

const NOT_FOUND = { digest: expect.stringContaining("404") };

async function createUser(role: "tenant" | "operator" = "tenant") {
  const id = randomUUID();
  await db.insert(user).values({ id, name: "Test", email: `${id}${DOMAIN}`, role });
  return id;
}

async function createMember() {
  const userId = await createUser();
  const [created] = await db
    .insert(tenant)
    .values({ name: "Project", slug: `${SLUG}${randomUUID()}` })
    .returning();
  await db.insert(tenantMember).values({ tenantId: created!.id, userId, role: "owner" });
  return { userId, tenantId: created!.id };
}

async function cleanup() {
  const ids = (await db.select({ id: tenant.id }).from(tenant).where(like(tenant.slug, `${SLUG}%`))).map(
    (t) => t.id,
  );
  if (ids.length > 0) {
    await db.delete(tenantMember).where(inArray(tenantMember.tenantId, ids));
    await db.delete(tenant).where(inArray(tenant.id, ids));
  }
  await db.delete(user).where(like(user.email, `%${DOMAIN}`));
}

beforeAll(async () => {
  await runMigrations();
  await cleanup();
});

beforeEach(() => {
  sessionUser = null;
});

afterAll(async () => {
  await cleanup();
  await sql.end();
});

describe("contexto do tenant", () => {
  it("vem da sessão: o tenant do membro logado", async () => {
    const { userId, tenantId } = await createMember();
    sessionUser = { id: userId, role: "tenant" };

    const ctx = await requireTenantContext();
    expect(ctx).toMatchObject({ userId, tenantId, memberRole: "owner", tenantStatus: "active" });
  });

  it("sem sessão vai para o login", async () => {
    await expect(requireTenantContext()).rejects.toMatchObject({ digest: "NEXT_REDIRECT;/sign-in" });
  });

  it("operador recebe 404 (D2)", async () => {
    sessionUser = { id: await createUser("operator"), role: "operator" };
    await expect(getTenantContext()).rejects.toMatchObject(NOT_FOUND);
    await expect(requireTenantContext()).rejects.toMatchObject(NOT_FOUND);
  });

  it("usuário sem projeto: nulo em getTenantContext e 404 em requireTenantContext", async () => {
    sessionUser = { id: await createUser(), role: "tenant" };
    expect(await getTenantContext()).toBeNull();
    await expect(requireTenantContext()).rejects.toMatchObject(NOT_FOUND);
  });
});

describe("contexto do operador", () => {
  it("só para o papel operator", async () => {
    const operatorId = await createUser("operator");
    sessionUser = { id: operatorId, role: "operator" };
    expect(await requireOperatorContext()).toEqual({ userId: operatorId });
  });

  it("membro de tenant recebe 404", async () => {
    const { userId } = await createMember();
    sessionUser = { id: userId, role: "tenant" };
    await expect(requireOperatorContext()).rejects.toMatchObject(NOT_FOUND);
  });
});
