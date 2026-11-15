// Cadastro self-service do projeto (#17) contra o Postgres de DATABASE_URL.
// A sessão é simulada.
import { randomUUID } from "node:crypto";
import { eq, inArray, like } from "drizzle-orm";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { requireOnboardingContext, requireTenantContext } from "@/lib/access";
import { createProject, getProject, ProjectError, slugify } from "@/lib/data/tenants";
import { db, sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { auditLog, tenant, tenantMember, user } from "@/lib/db/schema";

const DOMAIN = "@onboarding-test.notify.invalid";
// Todo projeto destes testes tem nome com este prefixo, para a limpeza.
const NAME = "Onboarding Test";

let sessionUser: { id: string; role: "tenant" | "operator"; emailVerified: boolean } | null = null;

vi.mock("@/lib/session", () => ({
  requireSession: async () => {
    if (!sessionUser) throw Object.assign(new Error("NEXT_REDIRECT"), { digest: "NEXT_REDIRECT;/sign-in" });
    return { user: { ...sessionUser, name: "Test", email: "test@example.invalid" } };
  },
}));

const NOT_FOUND = { digest: expect.stringContaining("404") };

async function createUser(role: "tenant" | "operator" = "tenant", emailVerified = true) {
  const id = randomUUID();
  await db.insert(user).values({ id, name: "Test", email: `${id}${DOMAIN}`, role, emailVerified });
  return id;
}

async function login(role: "tenant" | "operator" = "tenant", emailVerified = true) {
  const id = await createUser(role, emailVerified);
  sessionUser = { id, role, emailVerified };
  return id;
}

async function cleanup() {
  const ids = (await db.select({ id: tenant.id }).from(tenant).where(like(tenant.name, `${NAME}%`))).map(
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

describe("slug do projeto", () => {
  it("vira ASCII minúsculo com hífens", () => {
    expect(slugify("Soroban Explorer!")).toBe("soroban-explorer");
    expect(slugify("  Ação Única  ")).toBe("acao-unica");
    expect(slugify("🚀🚀")).toBe("project");
    expect(slugify("a".repeat(60))).toHaveLength(40);
  });
});

describe("quem pode criar projeto", () => {
  it("membro com e-mail confirmado e sem projeto", async () => {
    const id = await login();
    expect(await requireOnboardingContext()).toEqual({ userId: id });
  });

  it("e-mail não confirmado recebe 404", async () => {
    await login("tenant", false);
    await expect(requireOnboardingContext()).rejects.toMatchObject(NOT_FOUND);
  });

  it("operador recebe 404", async () => {
    await login("operator");
    await expect(requireOnboardingContext()).rejects.toMatchObject(NOT_FOUND);
  });

  it("quem já tem projeto vai para o painel", async () => {
    await login();
    await createProject(await requireOnboardingContext(), { name: `${NAME} existing` });
    await expect(requireOnboardingContext()).rejects.toMatchObject({
      digest: expect.stringContaining("/dashboard"),
    });
  });
});

describe("criação do projeto", () => {
  it("cria o tenant ativo com quem criou como owner e registra no audit_log", async () => {
    const userId = await login();
    const created = await createProject(await requireOnboardingContext(), { name: `  ${NAME} Alpha  ` });
    expect(created).toMatchObject({ name: `${NAME} Alpha`, slug: "onboarding-test-alpha" });

    const ctx = await requireTenantContext();
    expect(ctx).toMatchObject({ userId, tenantId: created.id, memberRole: "owner", tenantStatus: "active" });
    expect(await getProject(ctx)).toMatchObject({ name: `${NAME} Alpha`, status: "active" });

    const audit = await db.select().from(auditLog).where(eq(auditLog.tenantId, created.id));
    expect(audit).toMatchObject([{ action: "tenant.created", actorUserId: userId }]);
  });

  it("com SIGNUP_APPROVAL desligado (padrão ou `false`) o tenant nasce active, como sempre", async () => {
    for (const value of [undefined, "", "false", "0", "no"]) {
      if (value === undefined) delete process.env.SIGNUP_APPROVAL;
      else process.env.SIGNUP_APPROVAL = value;
      await login();
      const created = await createProject(await requireOnboardingContext(), { name: `${NAME} Off ${value ?? "unset"}` });
      expect(await requireTenantContext()).toMatchObject({ tenantId: created.id, tenantStatus: "active" });
      const [audit] = await db.select().from(auditLog).where(eq(auditLog.tenantId, created.id));
      expect(audit).toMatchObject({ action: "tenant.created", details: { status: "active" } });
    }
  });

  it("nome repetido em outro usuário ganha slug com sufixo", async () => {
    await login();
    const first = await createProject(await requireOnboardingContext(), { name: `${NAME} Same` });
    await login();
    const second = await createProject(await requireOnboardingContext(), { name: `${NAME} Same` });
    expect(second.slug).not.toBe(first.slug);
    expect(second.slug).toMatch(/^onboarding-test-same-[0-9a-f]{6}$/);
  });

  it("recusa nome curto demais", async () => {
    await login();
    await expect(createProject(await requireOnboardingContext(), { name: " x " })).rejects.toBeInstanceOf(
      ProjectError,
    );
  });

  it("dois pedidos ao mesmo tempo criam um projeto só (D1)", async () => {
    const userId = await login();
    const ctx = await requireOnboardingContext();
    const results = await Promise.allSettled([
      createProject(ctx, { name: `${NAME} Race A` }),
      createProject(ctx, { name: `${NAME} Race B` }),
    ]);
    expect(results.filter((r) => r.status === "fulfilled")).toHaveLength(1);
    const rejected = results.find((r) => r.status === "rejected") as PromiseRejectedResult;
    expect(rejected.reason).toMatchObject({ code: "already_member" });
    expect(await db.select().from(tenantMember).where(eq(tenantMember.userId, userId))).toHaveLength(1);
  });
});

describe("cadastro com aprovação (#49)", () => {
  afterEach(() => {
    delete process.env.SIGNUP_APPROVAL;
  });

  it("com SIGNUP_APPROVAL ligado o tenant nasce pending, com owner e audit_log", async () => {
    for (const value of ["true", "1", " TRUE "]) {
      process.env.SIGNUP_APPROVAL = value;
      const userId = await login();
      const created = await createProject(await requireOnboardingContext(), { name: `${NAME} On ${value.trim()}` });
      expect(await getProject((await requireTenantContext({ allowPending: true })))).toMatchObject({
        name: `${NAME} On ${value.trim()}`,
        status: "pending",
      });
      expect(await db.select().from(tenantMember).where(eq(tenantMember.tenantId, created.id))).toMatchObject([
        { userId, role: "owner" },
      ]);
      const [audit] = await db.select().from(auditLog).where(eq(auditLog.tenantId, created.id));
      expect(audit).toMatchObject({ action: "tenant.created", actorUserId: userId, details: { status: "pending" } });
    }
  });

  it("projeto pendente só passa por requireTenantContext com allowPending; o resto vai para /dashboard", async () => {
    process.env.SIGNUP_APPROVAL = "true";
    await login();
    await createProject(await requireOnboardingContext(), { name: `${NAME} Waiting` });
    await expect(requireTenantContext()).rejects.toMatchObject({ digest: expect.stringContaining("/dashboard") });
    expect(await requireTenantContext({ allowPending: true })).toMatchObject({ tenantStatus: "pending" });
  });
});
