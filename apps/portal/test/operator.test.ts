// Operador SCF (#19): regra D2 no banco, comando de administração, seed de
// desenvolvimento e visão global, contra o Postgres de DATABASE_URL.
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import path from "node:path";
import { promisify } from "node:util";
import { eq, inArray, like } from "drizzle-orm";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { OperatorContext } from "@/lib/access";
import { getTenantDetail, listTenants } from "@/lib/data/operator/tenants";
import { db, sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { apiService, auditLog, ingestKey, session, tenant, tenantMember, user } from "@/lib/db/schema";

const run = promisify(execFile);
const SCRIPTS = path.join(import.meta.dirname, "..", "scripts");
const DOMAIN = "@operator-test.notify.invalid";
const SLUG = "operator-test-";
const PASSWORD = "operator-password-123";
// Só o teste monta o contexto à mão; no portal ele vem de requireOperatorContext().
const operator = { userId: "test-operator" } as OperatorContext;

// Roda um script do portal com o banco dos testes e sem Umami.
async function script(name: string, args: string[], env: Record<string, string> = {}) {
  const base = { ...process.env };
  delete base.UMAMI_API_KEY;
  try {
    const { stdout, stderr } = await run("node", [path.join(SCRIPTS, name), ...args], {
      env: { ...base, ...env },
    });
    return { code: 0, out: stdout + stderr };
  } catch (error) {
    const e = error as { code: number; stdout: string; stderr: string };
    return { code: e.code, out: e.stdout + e.stderr };
  }
}

async function createUser(role: "tenant" | "operator" = "tenant") {
  const id = randomUUID();
  const email = `${id}${DOMAIN}`;
  await db.insert(user).values({ id, name: "Test", email, emailVerified: true, role });
  return { id, email };
}

async function createTenant(status: "active" | "removed" = "active") {
  const [created] = await db
    .insert(tenant)
    .values({ name: "Operator test", slug: `${SLUG}${randomUUID()}`, status })
    .returning();
  return created!;
}

async function cleanup() {
  const ids = (await db.select({ id: tenant.id }).from(tenant).where(like(tenant.slug, `${SLUG}%`))).map(
    (t) => t.id,
  );
  if (ids.length > 0) {
    await db.delete(ingestKey).where(inArray(ingestKey.tenantId, ids));
    await db.delete(apiService).where(inArray(apiService.tenantId, ids));
    await db.delete(tenantMember).where(inArray(tenantMember.tenantId, ids));
    await db.delete(tenant).where(inArray(tenant.id, ids));
  }
  await db.delete(user).where(like(user.email, `%${DOMAIN}`));
}

beforeAll(async () => {
  await runMigrations();
  await cleanup();
});

afterAll(async () => {
  await cleanup();
  await sql.end();
});

describe("operador não é membro de tenant (D2) no banco", () => {
  it("recusa pôr um operador num tenant", async () => {
    const op = await createUser("operator");
    const t = await createTenant();
    await expect(db.insert(tenantMember).values({ tenantId: t.id, userId: op.id })).rejects.toMatchObject({
      cause: { message: expect.stringContaining("D2") },
    });
  });

  it("recusa promover um membro de tenant a operador", async () => {
    const member = await createUser();
    const t = await createTenant();
    await db.insert(tenantMember).values({ tenantId: t.id, userId: member.id, role: "owner" });
    await expect(db.update(user).set({ role: "operator" }).where(eq(user.id, member.id))).rejects.toMatchObject({
      cause: { message: expect.stringContaining("D2") },
    });
  });
});

describe("comando de administração", () => {
  it("create cria operador com e-mail confirmado, sem tenant, e registra no audit_log", async () => {
    const email = `new-op${DOMAIN}`;
    const result = await script("operator.mjs", ["create", "--email", email, "--name", "SCF"], {
      OPERATOR_PASSWORD: PASSWORD,
    });
    expect(result).toMatchObject({ code: 0 });
    expect(result.out).not.toContain(PASSWORD);

    const [created] = await db.select().from(user).where(eq(user.email, email));
    expect(created).toMatchObject({ role: "operator", emailVerified: true });
    expect(await db.select().from(tenantMember).where(eq(tenantMember.userId, created!.id))).toEqual([]);
    const entries = (await db.select().from(auditLog).where(eq(auditLog.action, "operator.created"))).filter(
      (a) => (a.details as { userId?: string }).userId === created!.id,
    );
    // Ator nulo: a ação veio do comando de administração.
    expect(entries).toMatchObject([{ actorUserId: null }]);
  });

  it("create recusa senha curta", async () => {
    const result = await script("operator.mjs", ["create", "--email", `short${DOMAIN}`, "--name", "X"], {
      OPERATOR_PASSWORD: "short",
    });
    expect(result.code).toBe(1);
    expect(await db.select().from(user).where(eq(user.email, `short${DOMAIN}`))).toEqual([]);
  });

  it("promote e demote mudam o papel, registram e encerram as sessões", async () => {
    const target = await createUser();
    await db.insert(session).values({
      id: randomUUID(),
      token: randomUUID(),
      userId: target.id,
      expiresAt: new Date(Date.now() + 60_000),
    });

    expect((await script("operator.mjs", ["promote", "--email", target.email])).code).toBe(0);
    let [row] = await db.select().from(user).where(eq(user.id, target.id));
    expect(row!.role).toBe("operator");
    expect(await db.select().from(session).where(eq(session.userId, target.id))).toEqual([]);

    expect((await script("operator.mjs", ["demote", "--email", target.email])).code).toBe(0);
    [row] = await db.select().from(user).where(eq(user.id, target.id));
    expect(row!.role).toBe("tenant");

    const actions = (await db.select().from(auditLog))
      .filter((a) => (a.details as { userId?: string }).userId === target.id)
      .map((a) => a.action);
    expect(actions).toEqual(["operator.promoted", "operator.demoted"]);
  });

  it("promote recusa membro de tenant e não muda nada", async () => {
    const member = await createUser();
    const t = await createTenant();
    await db.insert(tenantMember).values({ tenantId: t.id, userId: member.id, role: "owner" });

    const result = await script("operator.mjs", ["promote", "--email", member.email]);
    expect(result.code).toBe(1);
    expect(result.out).toContain("D2");
    const [row] = await db.select().from(user).where(eq(user.id, member.id));
    expect(row!.role).toBe("tenant");
  });

  it("o cadastro de usuário comum não cria operador", async () => {
    const email = `plain${DOMAIN}`;
    const result = await script("create-user.mjs", ["--email", email, "--name", "X", "--role", "operator"], {
      PORTAL_USER_PASSWORD: PASSWORD,
    });
    // A opção --role não existe mais: o script recusa o argumento.
    expect(result.code).not.toBe(0);
    expect(await db.select().from(user).where(eq(user.email, email))).toEqual([]);
  });
});

describe("seed de desenvolvimento", () => {
  it("cria 2 projetos e 1 operador, e pode rodar de novo", async () => {
    const env = { SEED_PASSWORD: PASSWORD };
    expect((await script("seed.mjs", [], env)).code).toBe(0);
    const again = await script("seed.mjs", [], env);
    expect(again.code).toBe(0);
    expect(again.out).toContain("já existia");

    const seeded = await db.select().from(tenant).where(inArray(tenant.slug, ["seed-project-a", "seed-project-b"]));
    expect(seeded).toHaveLength(2);
    for (const t of seeded) {
      const members = await db.select().from(tenantMember).where(eq(tenantMember.tenantId, t.id));
      expect(members).toMatchObject([{ role: "owner" }]);
      expect(await db.select().from(ingestKey).where(eq(ingestKey.tenantId, t.id))).toHaveLength(1);
    }
    const [op] = await db.select().from(user).where(eq(user.email, "operator@seed.notify.localhost"));
    expect(op).toMatchObject({ role: "operator", emailVerified: true });
    expect(await db.select().from(tenantMember).where(eq(tenantMember.userId, op!.id))).toEqual([]);
  });
});

describe("visão global do operador", () => {
  it("lista todos os tenants, inclusive removidos, com as contagens", async () => {
    const active = await createTenant();
    const removed = await createTenant("removed");
    const owner = await createUser();
    await db.insert(tenantMember).values({ tenantId: active.id, userId: owner.id, role: "owner" });
    const [service] = await db.insert(apiService).values({ tenantId: active.id, name: "api" }).returning();
    await db.insert(ingestKey).values({
      apiServiceId: service!.id,
      tenantId: active.id,
      prefix: "opertest",
      keyHash: Buffer.alloc(32, 1),
      createdBy: owner.id,
    });

    const all = await listTenants(operator);
    expect(all.find((t) => t.id === active.id)).toMatchObject({
      status: "active",
      members: 1,
      sites: 0,
      services: 1,
      activeKeys: 1,
    });
    expect(all.find((t) => t.id === removed.id)).toMatchObject({ status: "removed", members: 0 });
  });

  it("detalha o tenant sem expor o hash das chaves", async () => {
    const t = await createTenant();
    const owner = await createUser();
    await db.insert(tenantMember).values({ tenantId: t.id, userId: owner.id, role: "owner" });
    const [service] = await db.insert(apiService).values({ tenantId: t.id, name: "detail-api" }).returning();
    await db.insert(ingestKey).values({
      apiServiceId: service!.id,
      tenantId: t.id,
      prefix: "detailkk",
      keyHash: Buffer.alloc(32, 2),
      createdBy: owner.id,
    });

    const detail = await getTenantDetail(operator, t.id);
    expect(detail!.members).toMatchObject([{ email: owner.email, role: "owner" }]);
    expect(detail!.services).toMatchObject([{ name: "detail-api", keys: [{ prefix: "detailkk" }] }]);
    expect(Object.keys(detail!.services[0]!.keys[0]!).sort()).toEqual([
      "createdAt",
      "lastUsedOn",
      "prefix",
      "revokedAt",
    ]);
    expect(await getTenantDetail(operator, randomUUID())).toBeNull();
    expect(await getTenantDetail(operator, "not-a-uuid")).toBeNull();
  });
});
