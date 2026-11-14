// Painel de API do tenant (#37): de qual tabela vem cada trecho do período, a
// leitura das métricas e a página, contra o Postgres de DATABASE_URL.
import { randomUUID } from "node:crypto";
import { inArray, like } from "drizzle-orm";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { loadTenantContext, type TenantContext } from "@/lib/access";
import { getServiceDashboard, metricSources } from "@/lib/data/api-metrics";
import { db, sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { apiService, tenant, tenantMember, user } from "@/lib/db/schema";
import { formatLatency, formatShare, statusLabel } from "@/lib/format";
import { resolvePeriod } from "@/lib/period";
import ServicePage from "@/app/(private)/services/[serviceId]/page";
import KeysPage from "@/app/(private)/services/[serviceId]/keys/page";

const DOMAIN = "@api-dashboard-test.notify.invalid";
const SLUG = "api-dashboard-test-";
const NOT_FOUND = { digest: expect.stringContaining("404") };
// hourCut = 09:00 e dayCut = 14/11 00:00 (UTC).
const NOW = new Date("2026-11-14T10:37:12.345Z");

let sessionUser: { id: string; role: "tenant" | "operator" } | null = null;
vi.mock("@/lib/session", () => ({
  requireSession: async () => {
    if (!sessionUser) throw Object.assign(new Error("NEXT_REDIRECT"), { digest: "NEXT_REDIRECT;/sign-in" });
    return { user: { ...sessionUser, name: "Test", email: "test@example.invalid", emailVerified: true } };
  },
}));

type Member = { userId: string; tenantId: string; ctx: TenantContext; serviceId: string };

async function createMember(status: "active" | "removed" = "active"): Promise<Member> {
  const userId = randomUUID();
  await db.insert(user).values({ id: userId, name: "Test", email: `${userId}${DOMAIN}`, emailVerified: true });
  const [t] = await db
    .insert(tenant)
    .values({ name: "Project", slug: `${SLUG}${randomUUID()}`, status, statusReason: status === "removed" ? "Spam" : null })
    .returning();
  await db.insert(tenantMember).values({ tenantId: t!.id, userId, role: "owner" });
  const [s] = await db.insert(apiService).values({ tenantId: t!.id, name: "horizon-proxy" }).returning();
  return { userId, tenantId: t!.id, ctx: (await loadTenantContext(userId))!, serviceId: s!.id };
}

// 15 posições, como api_metric_bounds() mais o bucket acima de 10 s.
function buckets(counts: Record<number, number>): number[] {
  return Array.from({ length: 15 }, (_, i) => counts[i] ?? 0);
}

type Point = { at: string; route?: string; method?: string; status?: number; count: number; b?: Record<number, number> };

async function insert(table: "api_metric_1m" | "api_metric_1h" | "api_metric_1d", m: Member, p: Point, serviceId = m.serviceId) {
  const b = buckets(p.b ?? { 2: p.count });
  await sql`
    INSERT INTO ${sql(table)} (bucket_start, tenant_id, api_service_id, route, method, status_code,
                               request_count, duration_sum, duration_buckets)
    VALUES (${p.at}, ${m.tenantId}, ${serviceId}, ${p.route ?? "/ledgers/{id}"}, ${p.method ?? "GET"},
            ${p.status ?? 200}, ${p.count}, ${p.count * 0.02}, ${b}::bigint[])`;
}

function text(html: string): string {
  return html
    .replace(/<[^>]+>/g, " ")
    .replace(/&#x27;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/\s+/g, " ");
}

async function render(element: Promise<ReactElement>): Promise<string> {
  return renderToStaticMarkup(await element);
}

function page(serviceId: string, period?: string) {
  return ServicePage({ params: Promise.resolve({ serviceId }), searchParams: Promise.resolve({ period }) });
}

async function cleanup() {
  const ids = (await db.select({ id: tenant.id }).from(tenant).where(like(tenant.slug, `${SLUG}%`))).map(
    (t) => t.id,
  );
  if (ids.length > 0) {
    for (const t of ["api_metric_1m", "api_metric_1h", "api_metric_1d"]) {
      await sql`DELETE FROM ${sql(t)} WHERE tenant_id IN ${sql(ids)}`;
    }
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

beforeEach(() => {
  sessionUser = null;
});

afterEach(() => {
  vi.useRealTimers();
});

afterAll(async () => {
  await cleanup();
  await sql.end();
});

describe("de qual tabela vem cada trecho", () => {
  const iso = (r: { from: Date; to: Date } | null) => r && { from: r.from.toISOString(), to: r.to.toISOString() };

  it("dias fechados do 1d, horas fechadas do 1h e as duas últimas horas do 1m", () => {
    const s = metricSources(resolvePeriod("7d", NOW));
    expect(iso(s.day)).toEqual({ from: "2026-11-08T00:00:00.000Z", to: "2026-11-14T00:00:00.000Z" });
    expect(iso(s.hour)).toEqual({ from: "2026-11-14T00:00:00.000Z", to: "2026-11-14T09:00:00.000Z" });
    expect(iso(s.minute)).toEqual({ from: "2026-11-14T09:00:00.000Z", to: NOW.toISOString() });
  });

  it("período por hora não usa o 1d", () => {
    const s = metricSources(resolvePeriod("24h", NOW));
    expect(s.day).toBeNull();
    expect(iso(s.hour)).toEqual({ from: "2026-11-13T11:00:00.000Z", to: "2026-11-14T09:00:00.000Z" });
    expect(iso(s.minute)).toEqual({ from: "2026-11-14T09:00:00.000Z", to: NOW.toISOString() });
  });

  it("logo depois da meia-noite, o dia anterior ainda vem do 1h", () => {
    const at = new Date("2026-11-14T00:30:00Z");
    const s = metricSources(resolvePeriod("12m", at));
    expect(iso(s.day)).toEqual({ from: "2025-12-01T00:00:00.000Z", to: "2026-11-13T00:00:00.000Z" });
    expect(iso(s.hour)).toEqual({ from: "2026-11-13T00:00:00.000Z", to: "2026-11-13T23:00:00.000Z" });
    expect(iso(s.minute)).toEqual({ from: "2026-11-13T23:00:00.000Z", to: at.toISOString() });
  });
});

describe("formatação", () => {
  it("latência em ms ou s, com teto de 10 s", () => {
    expect(formatLatency(null)).toBe("—");
    expect(formatLatency(0.0082)).toBe("8.2 ms");
    expect(formatLatency(0.375)).toBe("375 ms");
    expect(formatLatency(1.25)).toBe("1.25 s");
    expect(formatLatency(10)).toBe("≥ 10 s");
  });

  it("fração com uma casa abaixo de 10% e nunca zero quando houve erro", () => {
    expect(formatShare(5, 122)).toBe("4.1%");
    expect(formatShare(1, 5000)).toBe("< 0.1%");
    expect(formatShare(0, 10)).toBe("0.0%");
    expect(formatShare(50, 100)).toBe("50%");
    expect(formatShare(0, 0)).toBe("—");
  });

  it("status com o nome, 0 como desconhecido", () => {
    expect(statusLabel(404)).toBe("404 Not Found");
    expect(statusLabel(299)).toBe("299");
    expect(statusLabel(0)).toBe("Unknown");
  });
});

describe("leitura das métricas", () => {
  it("junta 1d, 1h e 1m sem contar nada duas vezes, só do serviço do tenant", async () => {
    const a = await createMember();
    const b = await createMember();
    const [otherService] = await db.insert(apiService).values({ tenantId: a.tenantId, name: "other" }).returning();

    // O que conta (período de 7 dias até NOW).
    await insert("api_metric_1d", a, { at: "2026-11-10T00:00:00Z", count: 100 });
    await insert("api_metric_1d", a, { at: "2026-11-10T00:00:00Z", status: 500, count: 5 });
    await insert("api_metric_1h", a, {
      at: "2026-11-14T05:00:00Z",
      route: "/accounts/{address}",
      method: "POST",
      status: 404,
      count: 10,
      b: { 7: 10 }, // (0.25, 0.5]
    });
    await insert("api_metric_1m", a, { at: "2026-11-14T09:30:00Z", count: 7 });

    // O que não pode contar: linhas que o consolidado já cobre, o dia corrente
    // parcial no 1d, a hora corrente no 1h, fora do período, outro serviço e
    // outro tenant.
    await insert("api_metric_1m", a, { at: "2026-11-10T12:00:00Z", count: 1000 });
    await insert("api_metric_1h", a, { at: "2026-11-10T12:00:00Z", count: 1000 });
    await insert("api_metric_1m", a, { at: "2026-11-14T05:10:00Z", count: 1000 });
    await insert("api_metric_1d", a, { at: "2026-11-14T00:00:00Z", count: 1000 });
    await insert("api_metric_1h", a, { at: "2026-11-14T09:00:00Z", count: 1000 });
    await insert("api_metric_1d", a, { at: "2026-11-07T00:00:00Z", count: 1000 });
    await insert("api_metric_1d", a, { at: "2026-11-10T00:00:00Z", count: 1000 }, otherService!.id);
    await insert("api_metric_1d", b, { at: "2026-11-10T00:00:00Z", count: 1000 });

    const data = (await getServiceDashboard(a.ctx, a.serviceId, resolvePeriod("7d", NOW)))!;
    expect(data.totals).toMatchObject({ requests: 122, clientErrors: 10, serverErrors: 5 });
    expect(data.series.map((p) => p.requests)).toEqual([0, 0, 105, 0, 0, 0, 17]);
    expect(data.series[0]).toMatchObject({ requests: 0, p50: null, p95: null });

    expect(data.endpointCount).toBe(2);
    expect(data.endpoints.map((e) => [e.method, e.route, e.requests, e.clientErrors, e.serverErrors])).toEqual([
      ["GET", "/ledgers/{id}", 112, 0, 5],
      ["POST", "/accounts/{address}", 10, 10, 0],
    ]);
    // Um bucket só: interpolação linear dentro de (0.25, 0.5].
    expect(data.endpoints[1]!.p50).toBeCloseTo(0.375, 6);
    expect(data.endpoints[1]!.p95).toBeCloseTo(0.4875, 6);
    expect(data.statuses).toEqual([
      { statusCode: 200, requests: 107 },
      { statusCode: 404, requests: 10 },
      { statusCode: 500, requests: 5 },
    ]);

    // Serviço de outro tenant: nulo, sem ler métricas.
    expect(await getServiceDashboard(a.ctx, b.serviceId, resolvePeriod("7d", NOW))).toBeNull();
  });

  it("período por hora lê o 1h mesmo onde já existe o dia consolidado", async () => {
    const a = await createMember();
    await insert("api_metric_1h", a, { at: "2026-11-13T20:00:00Z", count: 3 });
    await insert("api_metric_1d", a, { at: "2026-11-13T00:00:00Z", count: 1000 });
    const data = (await getServiceDashboard(a.ctx, a.serviceId, resolvePeriod("24h", NOW)))!;
    expect(data.series).toHaveLength(24);
    expect(data.totals.requests).toBe(3);
    expect(data.series.find((p) => p.requests > 0)!.at.toISOString()).toBe("2026-11-13T20:00:00.000Z");
  });

  it("sem dados: zeros, percentis nulos e listas vazias", async () => {
    const a = await createMember();
    const data = (await getServiceDashboard(a.ctx, a.serviceId, resolvePeriod("30d", NOW)))!;
    expect(data.totals).toEqual({ requests: 0, clientErrors: 0, serverErrors: 0, p50: null, p95: null });
    expect(data.series).toHaveLength(30);
    expect(data.endpoints).toEqual([]);
    expect(data.statuses).toEqual([]);
  });
});

describe("página", () => {
  it("mostra totais, gráficos, endpoints e status do período", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: NOW });
    const a = await createMember();
    await insert("api_metric_1d", a, { at: "2026-11-10T00:00:00Z", count: 100 });
    await insert("api_metric_1d", a, { at: "2026-11-10T00:00:00Z", status: 500, count: 5 });
    await insert("api_metric_1h", a, {
      at: "2026-11-14T05:00:00Z",
      route: "/accounts/{address}",
      method: "POST",
      status: 404,
      count: 10,
      b: { 7: 10 },
    });
    await insert("api_metric_1m", a, { at: "2026-11-14T09:30:00Z", count: 7 });
    sessionUser = { id: a.userId, role: "tenant" };

    const html = await render(page(a.serviceId));
    const t = text(html);
    expect(t).toContain("Requests 122");
    expect(t).toContain("Server errors (5xx) 4.1% 5 requests");
    expect(t).toMatch(/Median latency \(p50\) \d/);
    expect(t).toContain("GET /ledgers/{id} 112");
    expect(t).toContain("POST /accounts/{address} 10 100% 0.0% 375 ms 488 ms");
    expect(t).toContain("404 Not Found 10 · 8.2%");
    expect(t).toContain("2 endpoints, by requests");
    expect(html).toMatch(/aria-current="page"[^>]*>Last 7 days</);
    // Uma coluna e um ponto de latência por dia, e a tabela equivalente.
    expect(html.match(/aria-label="[^"]+: \d+ requests?, \d+ server errors?"/g)).toHaveLength(7);
    expect(html).toContain("Tue, Nov 10, 2026: 105 requests, 5 server errors");
    expect(html.match(/aria-label="[^"]+: (p50|no requests)/g)).toHaveLength(7);
    expect(html).toContain("Tue, Nov 10, 2026: p50 ");
    expect(t).not.toContain("No requests in this period yet");
  });

  it("serviço sem dados mostra como começar", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: NOW });
    const a = await createMember();
    sessionUser = { id: a.userId, role: "tenant" };
    const t = text(await render(page(a.serviceId, "24h")));
    expect(t).toContain("No requests in this period yet");
    expect(t).toContain("Requests 0");
    expect(t).toContain("Median latency (p50) —");
  });

  it("serviço de outro tenant é 404 no painel e nas chaves", async () => {
    const a = await createMember();
    const b = await createMember();
    sessionUser = { id: a.userId, role: "tenant" };
    await expect(page(b.serviceId)).rejects.toMatchObject(NOT_FOUND);
    await expect(KeysPage({ params: Promise.resolve({ serviceId: b.serviceId }) })).rejects.toMatchObject(NOT_FOUND);
    await expect(page("not-a-uuid")).rejects.toMatchObject(NOT_FOUND);
  });

  it("projeto removido vê só o aviso, sem números", async () => {
    const r = await createMember("removed");
    await insert("api_metric_1m", r, { at: new Date(Date.now() - 60_000).toISOString(), count: 9 });
    sessionUser = { id: r.userId, role: "tenant" };
    const t = text(await render(page(r.serviceId)));
    expect(t).toContain("This project was removed by the SCF operators.");
    expect(t).toContain("Reason: Spam");
    expect(t).not.toContain("Requests");
  });
});
