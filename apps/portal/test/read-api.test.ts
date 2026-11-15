// Chaves de leitura do SCF e API de leitura (#48, ADR 0006) contra o Postgres
// de DATABASE_URL e um Umami falso. Os casos de isolamento (tenant, ingestão,
// coletor) ficam em isolation.test.ts.
import { randomUUID } from "node:crypto";
import { desc, eq, inArray, like } from "drizzle-orm";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { OperatorContext } from "@/lib/access";
import {
  createReadKey,
  listReadKeys,
  MAX_ACTIVE_READ_KEYS,
  ReadKeyError,
  revokeReadKey,
} from "@/lib/data/operator/api-keys";
import { sql, db } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { auditLog, operatorApiKey, site, tenant, user } from "@/lib/db/schema";
import { generateIngestKey } from "@/lib/ingest-key";
import { generateReadKey, hashReadKey, READ_KEY_PATTERN } from "@/lib/read-key";
import { READ_API_LIMIT, resetRateLimits, takeToken } from "@/lib/rate-limit";
import { truncatedClientIp } from "@/lib/client-ip";
import { LOOKUP_LIMIT_PER_IP } from "@/lib/read-api";
import { GET as getApi } from "@/app/api/v1/api/route";
import { GET as getProjects } from "@/app/api/v1/projects/route";
import { GET as getWeb } from "@/app/api/v1/web/route";
import { startFakeUmami, type FakeUmami } from "./fake-umami";

// O segredo é tudo depois de `nrk_<prefixo>_`. Ele é base64url e pode conter `_`,
// então cortar a chave por `_` e pegar o terceiro pedaço dá um fragmento curto
// que aparece em qualquer JSON e deixa o teste instável.
function secretOf(key: string): string {
  return key.slice(key.indexOf("_", "nrk_".length) + 1);
}

const DOMAIN = "@read-api-test.notify.invalid";
const SLUG = "read-api-test-";

let umami: FakeUmami;
let operatorId: string;
let op: OperatorContext;
let activeTenantId: string;
let removedTenantId: string;

const call = (route: typeof getWeb, path: string, key?: string, header?: string, extra: Record<string, string> = {}) =>
  route(
    new Request(`http://portal.test${path}`, {
      headers: { ...(key || header ? { authorization: header ?? `Bearer ${key}` } : {}), ...extra },
    }),
  );

async function newKey(name = "test") {
  return createReadKey(op, { name });
}

async function cleanup() {
  const keys = (await db.select({ id: user.id }).from(user).where(like(user.email, `%${DOMAIN}`))).map((u) => u.id);
  if (keys.length > 0) await db.delete(operatorApiKey).where(inArray(operatorApiKey.createdBy, keys));
  const ids = (await db.select({ id: tenant.id }).from(tenant).where(like(tenant.slug, `${SLUG}%`))).map((t) => t.id);
  if (ids.length > 0) {
    await db.delete(site).where(inArray(site.tenantId, ids));
    await db.delete(tenant).where(inArray(tenant.id, ids));
  }
  await db.delete(user).where(like(user.email, `%${DOMAIN}`));
}

beforeAll(async () => {
  await runMigrations();
  await cleanup();
  umami = await startFakeUmami(process.env.UMAMI_API_KEY!);
  process.env.UMAMI_URL = umami.url;
  operatorId = randomUUID();
  await db.insert(user).values({ id: operatorId, name: "Op", email: `${operatorId}${DOMAIN}`, role: "operator" });
  op = { userId: operatorId } as OperatorContext;
  const [a] = await db.insert(tenant).values({ name: "Active Project", slug: `${SLUG}active` }).returning();
  const [r] = await db
    .insert(tenant)
    .values({ name: "Removed Project", slug: `${SLUG}removed`, status: "removed" })
    .returning();
  activeTenantId = a!.id;
  removedTenantId = r!.id;
  for (const t of [a!, r!]) {
    const umamiWebsiteId = randomUUID();
    umami.websites.set(umamiWebsiteId, { id: umamiWebsiteId, name: t.slug, domain: `${t.slug}.example` });
    await db.insert(site).values({ tenantId: t.id, name: t.slug, domain: `${t.slug}.example`, umamiWebsiteId });
  }
});

beforeEach(() => {
  resetRateLimits();
  umami.requests.length = 0;
});

afterAll(async () => {
  await cleanup();
  await umami.close();
  await sql.end();
});

describe("formato da chave", () => {
  it("é nrk_<prefixo>_<segredo> e nunca parece uma chave de ingestão", () => {
    const { key, prefix, keyHash } = generateReadKey();
    expect(key).toMatch(READ_KEY_PATTERN);
    expect(key.startsWith(`nrk_${prefix}_`)).toBe(true);
    expect(keyHash).toEqual(hashReadKey(key));
    expect(generateIngestKey().key).not.toMatch(READ_KEY_PATTERN);
    expect(key).not.toMatch(/^ntf_/);
  });
});

describe("gerar, listar e revogar", () => {
  it("guarda só o hash, lista sem segredo e escreve no audit_log", async () => {
    const created = await newKey("Grafana");
    expect(created.key).toMatch(READ_KEY_PATTERN);
    expect(created.summary).toMatchObject({ name: "Grafana", createdByMe: true, revokedAt: null, lastUsedOn: null });

    const [row] = await db.select().from(operatorApiKey).where(eq(operatorApiKey.id, created.summary.id));
    expect(row!.keyHash).toEqual(hashReadKey(created.key));
    // A chave completa não está em nenhuma coluna de texto da tabela nem do audit_log.
    const secret = secretOf(created.key);
    const dump = JSON.stringify([
      row,
      await db.select().from(auditLog).where(eq(auditLog.actorUserId, operatorId)),
    ]);
    expect(dump).not.toContain(secret);
    expect(JSON.stringify(await listReadKeys(op))).not.toContain(secret);

    const [audit] = await db
      .select()
      .from(auditLog)
      .where(eq(auditLog.action, "operator_api_key.created"))
      .orderBy(desc(auditLog.at))
      .limit(1);
    expect(audit).toMatchObject({ actorUserId: operatorId, tenantId: null });
    expect(audit!.details).toMatchObject({ keyId: created.summary.id, prefix: created.summary.prefix });
  });

  it("valida o nome", async () => {
    await expect(createReadKey(op, { name: "   " })).rejects.toBeInstanceOf(ReadKeyError);
    await expect(createReadKey(op, { name: "x".repeat(101) })).rejects.toMatchObject({ code: "invalid_name" });
  });

  it("revoga uma vez, trata repetição e ID desconhecido, e audita", async () => {
    const { summary } = await newKey("to-revoke");
    expect(await revokeReadKey(op, summary.id)).toBe(true);
    expect(await revokeReadKey(op, summary.id)).toBe(false);
    expect(await revokeReadKey(op, randomUUID())).toBeNull();
    expect(await revokeReadKey(op, "não-é-uuid")).toBeNull();
    const [audit] = await db
      .select()
      .from(auditLog)
      .where(eq(auditLog.action, "operator_api_key.revoked"))
      .orderBy(desc(auditLog.at))
      .limit(1);
    expect(audit!.details).toMatchObject({ keyId: summary.id });
    expect((await listReadKeys(op)).find((k) => k.id === summary.id)?.revokedAt).not.toBeNull();
  });

  it("limita as chaves ativas", async () => {
    await db.update(operatorApiKey).set({ revokedAt: new Date() });
    for (let i = 0; i < MAX_ACTIVE_READ_KEYS; i++) await newKey(`k${i}`);
    await expect(newKey("one-more")).rejects.toMatchObject({ code: "key_limit" });
    await db.update(operatorApiKey).set({ revokedAt: new Date() });
  });
});

describe("autenticação da API", () => {
  async function snapshot(res: Response) {
    return { status: res.status, body: await res.text(), headers: [...res.headers].sort() };
  }

  it("toda recusa tem status, corpo e cabeçalhos idênticos, sem pista do motivo", async () => {
    const good = await newKey("good");
    const revoked = await newKey("revoked-401");
    await revokeReadKey(op, revoked.summary.id);
    const exId = randomUUID();
    await db.insert(user).values({ id: exId, name: "Ex", email: `${exId}${DOMAIN}`, role: "operator" });
    const ex = await createReadKey({ userId: exId } as OperatorContext, { name: "ex" });
    await sql`UPDATE "user" SET role = 'tenant' WHERE id = ${exId}`;
    const wrongSecret = `${good.key.slice(0, -1)}${good.key.endsWith("A") ? "B" : "A"}`;

    const attempts: [string, string | undefined, string | undefined][] = [
      ["sem header", undefined, undefined],
      ["esquema errado", undefined, "Basic abc"],
      ["bearer sem chave", undefined, "Bearer "],
      ["formato errado", "nrk_ABCDEFGH_x", undefined],
      ["chave de ingestão", `ntf_abcdefgh_${"A".repeat(43)}`, undefined],
      ["prefixo desconhecido", `nrk_zzzzzzzz_${"A".repeat(43)}`, undefined],
      ["segredo errado", wrongSecret, undefined],
      ["chave revogada", revoked.key, undefined],
      ["ex-operador", ex.key, undefined],
    ];
    const results = [];
    for (const [name, key, header] of attempts) {
      const res = await call(getProjects, "/api/v1/projects", key, header);
      expect(res.status, name).toBe(401);
      expect(res.headers.get("www-authenticate"), name).toMatch(/^Bearer/);
      expect(res.headers.get("cache-control"), name).toBe("no-store");
      results.push(await snapshot(res));
    }
    expect(JSON.parse(results[0]!.body)).toEqual({
      error: { code: "unauthorized", message: "Missing or invalid API key." },
    });
    for (const r of results) expect(r).toEqual(results[0]);
    await sql`UPDATE "user" SET role = 'operator' WHERE id = ${exId}`;
  });

  it("aceita o esquema Bearer sem diferenciar maiúsculas (RFC 7235)", async () => {
    const { key } = await newKey("case");
    for (const scheme of ["bearer", "BEARER", "BeArEr"]) {
      expect((await call(getProjects, "/api/v1/projects", undefined, `${scheme} ${key}`)).status, scheme).toBe(200);
    }
  });

  it("falha ao gravar o último uso não derruba uma chave válida e não vaza a chave", async () => {
    const { key, summary } = await newKey("write-fails");
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const update = vi.spyOn(db, "update").mockImplementationOnce(() => {
      throw new Error(`falha com ${key}`);
    });
    const res = await call(getProjects, "/api/v1/projects", key);
    expect(res.status).toBe(200);
    expect(update).toHaveBeenCalledTimes(1);
    expect(spy).toHaveBeenCalledWith("Falha ao gravar o último uso da chave de leitura", "Error");
    expect(JSON.stringify(spy.mock.calls)).not.toContain(secretOf(key));
    expect((await listReadKeys(op)).find((k) => k.id === summary.id)?.lastUsedOn).toBeNull();
    update.mockRestore();
    spy.mockRestore();
  });

  it("segredo errado com prefixo certo: 401", async () => {
    const { key } = await newKey();
    const wrong = `${key.slice(0, -1)}${key.endsWith("A") ? "B" : "A"}`;
    expect((await call(getProjects, "/api/v1/projects", wrong)).status).toBe(401);
    expect((await call(getProjects, "/api/v1/projects", key)).status).toBe(200);
  });

  it("chave revogada falha e a chave de quem deixou de ser operador também", async () => {
    const { key, summary } = await newKey("revoked");
    expect((await call(getProjects, "/api/v1/projects", key)).status).toBe(200);
    await revokeReadKey(op, summary.id);
    expect((await call(getProjects, "/api/v1/projects", key)).status).toBe(401);

    const other = randomUUID();
    await db.insert(user).values({ id: other, name: "Op2", email: `${other}${DOMAIN}`, role: "operator" });
    const second = await createReadKey({ userId: other } as OperatorContext, { name: "second" });
    expect((await call(getProjects, "/api/v1/projects", second.key)).status).toBe(200);
    await sql`UPDATE "user" SET role = 'tenant' WHERE id = ${other}`;
    expect((await call(getProjects, "/api/v1/projects", second.key)).status).toBe(401);
    await sql`UPDATE "user" SET role = 'operator' WHERE id = ${other}`;
    expect((await call(getProjects, "/api/v1/projects", second.key)).status).toBe(200);
  });

  it("grava o último uso no máximo uma vez por dia", async () => {
    const { key, summary } = await newKey("used");
    await call(getProjects, "/api/v1/projects", key);
    const today = new Date().toISOString().slice(0, 10);
    expect((await listReadKeys(op)).find((k) => k.id === summary.id)?.lastUsedOn).toBe(today);
  });

  it("não escreve a chave no log em caso de erro", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { key } = await newKey("log");
    await call(getProjects, "/api/v1/projects", key);
    await call(getProjects, "/api/v1/projects", `${key}x`);
    expect(JSON.stringify(spy.mock.calls)).not.toContain(secretOf(key));
    spy.mockRestore();
  });
});

describe("limite de requisições", () => {
  it("devolve 429 com Retry-After depois do limite, por chave", async () => {
    const a = await newKey("a");
    const b = await newKey("b");
    for (let i = 0; i < READ_API_LIMIT; i++) {
      expect((await call(getProjects, "/api/v1/projects", a.key)).status).toBe(200);
    }
    const limited = await call(getProjects, "/api/v1/projects", a.key);
    expect(limited.status).toBe(429);
    expect(Number(limited.headers.get("retry-after"))).toBeGreaterThan(0);
    // Outra chave não é afetada.
    expect((await call(getProjects, "/api/v1/projects", b.key)).status).toBe(200);
  });

  it("a janela reabre depois de um minuto", () => {
    const t0 = 1_000_000;
    for (let i = 0; i < 3; i++) expect(takeToken("w", t0, 3).ok).toBe(true);
    expect(takeToken("w", t0 + 1000, 3)).toMatchObject({ ok: false });
    expect(takeToken("w", t0 + 60_000, 3).ok).toBe(true);
  });
});

describe("limite por IP antes da consulta", () => {
  const withIp = (ip: string, key: string) => call(getProjects, "/api/v1/projects", key, undefined, { "x-forwarded-for": ip });
  const unknown = `nrk_zzzzzzzz_${"A".repeat(43)}`;

  it("depois do limite, chutes de chave dão 429 sem consultar o banco; outro IP segue", async () => {
    const select = vi.spyOn(db, "select");
    for (let i = 0; i < LOOKUP_LIMIT_PER_IP; i++) expect((await withIp("203.0.113.7", unknown)).status).toBe(401);
    expect(select).toHaveBeenCalledTimes(LOOKUP_LIMIT_PER_IP);
    const limited = await withIp("203.0.113.7", unknown);
    expect(limited.status).toBe(429);
    expect(Number(limited.headers.get("retry-after"))).toBeGreaterThan(0);
    expect(select).toHaveBeenCalledTimes(LOOKUP_LIMIT_PER_IP);
    // Mesmo /24 conta junto; outro IP não.
    expect((await withIp("203.0.113.200", unknown)).status).toBe(429);
    expect((await withIp("198.51.100.9", unknown)).status).toBe(401);
    select.mockRestore();
  });

  it("header sem o formato de chave não gasta o limite nem toca no banco", async () => {
    const select = vi.spyOn(db, "select");
    for (let i = 0; i < LOOKUP_LIMIT_PER_IP + 5; i++) {
      expect((await call(getProjects, "/api/v1/projects", "lixo", undefined, { "x-forwarded-for": "192.0.2.1" })).status).toBe(401);
    }
    expect(select).not.toHaveBeenCalled();
    select.mockRestore();
  });

  it("vale a última entrada do X-Forwarded-For (a do Caddy), não a que o cliente escreve", () => {
    expect(truncatedClientIp("1.2.3.4, 203.0.113.7")).toBe("203.0.113.0");
    expect(truncatedClientIp("203.0.113.7")).toBe("203.0.113.0");
    expect(truncatedClientIp("2001:db8:abcd:12:1:2:3:4")).toBe("2001:db8:abcd::/48");
    expect(truncatedClientIp("2001:db8::1")).toBe("2001:db8:0::/48");
    expect(truncatedClientIp("não é ip")).toBe("unknown");
    expect(truncatedClientIp(null)).toBe("unknown");
  });
});

describe("respostas", () => {
  it("/projects lista só projetos ativos, sem ID do Umami nem dado pessoal", async () => {
    const { key } = await newKey();
    const res = await call(getProjects, "/api/v1/projects", key);
    const body = (await res.json()) as { projects: { id: string; name: string; slug: string }[] };
    expect(body.projects.find((p) => p.id === activeTenantId)).toMatchObject({
      name: "Active Project",
      websites: 1,
      apiServices: 0,
    });
    expect(body.projects.find((p) => p.id === removedTenantId)).toBeUndefined();
    const text = JSON.stringify(body);
    expect(text).not.toContain("@");
    for (const id of umami.websites.keys()) expect(text).not.toContain(id);
  });

  it("/web traz os totais do painel e deixa projeto removido de fora, sem chamá-lo no Umami", async () => {
    const { key } = await newKey();
    const res = await call(getWeb, "/api/v1/web?period=30d", key);
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      period: { key: string };
      projects: { id: string; totals: { pageviews: number } | null }[];
    };
    expect(body.period.key).toBe("30d");
    expect(body.projects.find((p) => p.id === activeTenantId)?.totals).toMatchObject({ pageviews: 7 });
    expect(body.projects.find((p) => p.id === removedTenantId)).toBeUndefined();
    const removedWebsite = [...umami.websites.values()].find((w) => w.name === `${SLUG}removed`)!;
    expect(umami.requests.some((r) => r.path.includes(removedWebsite.id))).toBe(false);
  });

  it("/api traz volume e latência dos projetos ativos", async () => {
    const { key } = await newKey();
    const res = await call(getApi, "/api/v1/api", key);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { period: { key: string }; projects: { id: string; totals: { requests: number } }[] };
    expect(body.period.key).toBe("7d");
    expect(body.projects.find((p) => p.id === activeTenantId)?.totals).toMatchObject({ requests: 0, p95: null });
    expect(body.projects.find((p) => p.id === removedTenantId)).toBeUndefined();
  });

  it("período desconhecido: 400, e nada chega ao Umami", async () => {
    const { key } = await newKey();
    const res = await call(getWeb, "/api/v1/web?period=1y", key);
    expect(res.status).toBe(400);
    expect(umami.requests).toEqual([]);
  });

  it("falha do Umami num site: o projeto vem com totals nulo", async () => {
    const { key } = await newKey();
    umami.down = true;
    try {
      const body = (await (await call(getWeb, "/api/v1/web", key)).json()) as {
        unavailable: number;
        projects: { id: string; totals: unknown }[];
      };
      expect(body.unavailable).toBeGreaterThan(0);
      expect(body.projects.find((p) => p.id === activeTenantId)?.totals).toBeNull();
    } finally {
      umami.down = false;
    }
  });
});
