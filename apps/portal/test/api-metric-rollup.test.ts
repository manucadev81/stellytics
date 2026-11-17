// Teste de integração da consolidação e da retenção das métricas de API (#34,
// spec do E5, seção 5) contra um Postgres real (DATABASE_URL).
import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { asRole } from "./helpers/as-role";

const tenantId = randomUUID();
const serviceId = randomUUID();
const tables = ["api_metric_1m", "api_metric_1h", "api_metric_1d"];

function buckets(counts: Record<number, number>): string {
  return `{${Array.from({ length: 15 }, (_, i) => counts[i] ?? 0).join(",")}}`;
}

// O upsert do coletor (#31) no balde de 1 minuto.
async function point(at: string, route: string, count: number, sum: number, b: Record<number, number>) {
  await sql`
    INSERT INTO api_metric_1m AS m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                                    request_count, duration_sum, duration_buckets)
    VALUES (${at}, ${tenantId}, ${serviceId}, ${route}, 'GET', 200, ${count}, ${sum}, ${buckets(b)}::bigint[])
    ON CONFLICT (tenant_id, api_service_id, route, method, status_code, bucket_start) DO UPDATE SET
      request_count = m.request_count + EXCLUDED.request_count,
      duration_sum = m.duration_sum + EXCLUDED.duration_sum,
      duration_buckets = api_metric_add_buckets(m.duration_buckets, EXCLUDED.duration_buckets)`;
}

async function rows(table: string) {
  const r = await sql.unsafe<{ at: string; route: string; n: string; sum: number; b: string[] }[]>(
    `SELECT to_char(bucket_start AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI') AS at, route,
            request_count AS n, duration_sum AS sum, duration_buckets AS b
     FROM ${table} WHERE tenant_id = $1 ORDER BY bucket_start, route`,
    [tenantId],
  );
  return r.map((x) => ({ at: x.at, route: x.route, n: Number(x.n), sum: x.sum, b: x.b.map(Number) }));
}

const rollup = (since: string, until: string) => sql`SELECT api_metric_rollup(${since}, ${until})`;

beforeAll(async () => {
  await runMigrations();
});

afterAll(async () => {
  for (const t of tables) await sql.unsafe(`DELETE FROM ${t} WHERE tenant_id = $1`, [tenantId]);
  await sql.end();
});

describe("consolidação", () => {
  // Datas antigas caem na partição default, longe dos dados dos outros testes.
  it("soma as horas fechadas em 1h e 1d e é idempotente", async () => {
    await point("2020-03-10T10:00:00Z", "/a", 2, 0.5, { 3: 2 });
    await point("2020-03-10T10:59:00Z", "/a", 3, 1.5, { 3: 1, 9: 2 });
    await point("2020-03-10T11:30:00Z", "/a", 1, 0.1, { 5: 1 });
    // Hora ainda aberta: termina menos de 5 minutos antes do `until`.
    await point("2020-03-10T12:10:00Z", "/a", 7, 7, { 10: 7 });

    await rollup("2020-03-10T00:00:00Z", "2020-03-10T13:04:00Z");
    await rollup("2020-03-10T00:00:00Z", "2020-03-10T13:04:00Z");

    expect(await rows("api_metric_1h")).toEqual([
      { at: "2020-03-10T10:00", route: "/a", n: 5, sum: 2, b: [0, 0, 0, 3, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0] },
      { at: "2020-03-10T11:00", route: "/a", n: 1, sum: 0.1, b: [0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0] },
    ]);
    expect(await rows("api_metric_1d")).toEqual([
      { at: "2020-03-10T00:00", route: "/a", n: 6, sum: 2.1, b: [0, 0, 0, 3, 0, 1, 0, 0, 0, 2, 0, 0, 0, 0, 0] },
    ]);
  });

  it("substitui pelo valor recalculado quando chega um lote atrasado", async () => {
    await point("2020-03-10T10:30:00Z", "/a", 4, 0, { 0: 4 });
    await rollup("2020-03-10T10:00:00Z", "2020-03-10T13:05:00Z");
    await rollup("2020-03-10T10:00:00Z", "2020-03-10T13:05:00Z");

    const hours = await rows("api_metric_1h");
    expect(hours.map((h) => [h.at, h.n])).toEqual([
      ["2020-03-10T10:00", 9],
      ["2020-03-10T11:00", 1],
      ["2020-03-10T12:00", 7],
    ]);
    const [day] = await rows("api_metric_1d");
    expect(day!.n).toBe(17);
    expect(day!.b.reduce((a, x) => a + x, 0)).toBe(17);
  });

  it("não faz nada sem hora fechada", async () => {
    await rollup("2020-03-11T10:00:00Z", "2020-03-11T11:04:59Z");
    expect((await rows("api_metric_1h")).filter((h) => h.at.startsWith("2020-03-11"))).toEqual([]);
  });

  it("api_metric_maintain roda inteira como stellytics", async () => {
    await sql`SELECT api_metric_maintain()`;
  });

  it("o coletor não roda a consolidação nem a retenção", async () => {
    for (const q of [
      "SELECT api_metric_maintain()",
      "SELECT api_metric_rollup(now() - interval '1 day')",
      "SELECT api_metric_drop_partitions()",
    ]) {
      await expect(asRole("stellytics_collector", q)).rejects.toThrow(/permission denied/);
    }
  });
});

describe("retenção", () => {
  it("apaga as partições mensais antigas e mantém as recentes e a default", async () => {
    // Partições de janeiro de 2000 nas três tabelas, com uma linha cada.
    for (const t of tables) {
      await sql.unsafe(
        `CREATE TABLE IF NOT EXISTS ${t}_p200001 PARTITION OF ${t}
         FOR VALUES FROM ('2000-01-01T00:00:00Z') TO ('2000-02-01T00:00:00Z')`,
      );
      await sql.unsafe(
        `INSERT INTO ${t} (bucket_start, tenant_id, api_service_id, route, method, status_code,
                           request_count, duration_sum, duration_buckets)
         VALUES ('2000-01-15T00:00:00Z', $1, $2, '/old', 'GET', 200, 1, 0, $3::bigint[])`,
        [tenantId, serviceId, buckets({ 0: 1 })],
      );
    }

    // 1m vive 35 dias depois do fim do mês; 1h e 1d, 25 meses.
    const at = (when: string) => sql<{ name: string }[]>`SELECT api_metric_drop_partitions(${when}) AS name`;
    expect((await at("2000-03-06T23:00:00Z")).map((r) => r.name)).not.toContain("api_metric_1m_p200001");
    expect((await at("2000-03-07T00:00:00Z")).map((r) => r.name)).toEqual(["api_metric_1m_p200001"]);
    expect(await at("2002-02-28T00:00:00Z")).toEqual([]);
    expect((await at("2002-03-01T00:00:00Z")).map((r) => r.name).sort()).toEqual([
      "api_metric_1d_p200001",
      "api_metric_1h_p200001",
    ]);

    for (const t of tables) {
      expect(await sql`SELECT to_regclass(${t + "_p200001"}) AS r`).toEqual([{ r: null }]);
      expect(await sql`SELECT to_regclass(${t + "_default"})::text AS r`).toEqual([{ r: `${t}_default` }]);
    }
    // Com o relógio de hoje, nada do mês corrente ou dos seguintes some.
    const now = new Date();
    const current = `${now.getUTCFullYear()}${String(now.getUTCMonth() + 1).padStart(2, "0")}`;
    await sql`SELECT api_metric_drop_partitions()`;
    for (const t of tables) {
      expect(await sql`SELECT to_regclass(${`${t}_p${current}`})::text AS r`).toEqual([{ r: `${t}_p${current}` }]);
    }
  });
});
