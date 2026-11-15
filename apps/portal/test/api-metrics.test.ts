// Teste de integração das tabelas de métricas de API (#33, spec do E5, seção 4)
// contra um Postgres real (DATABASE_URL). Exige os papéis de
// infra/compose/postgres/init/02-ingest-roles.sh.
import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { sql } from "@/lib/db";
import { runMigrations } from "@/lib/db/migrate";
import { asRole } from "./helpers/as-role";

// Métricas não têm chave estrangeira (spec, seção 4): IDs soltos bastam.
const tenantId = randomUUID();
const serviceId = randomUUID();

function buckets(counts: Record<number, number>): number[] {
  return Array.from({ length: 15 }, (_, i) => counts[i] ?? 0);
}

function arrayLiteral(values: number[]) {
  return `'{${values.join(",")}}'::bigint[]`;
}

// O upsert que o coletor faz (#31): soma contagem, soma e buckets.
function upsert(table: string, at: string, route: string, status: number, count: number, sum: number, b: number[]) {
  return `
    INSERT INTO ${table} AS m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                               request_count, duration_sum, duration_buckets)
    VALUES ('${at}', '${tenantId}', '${serviceId}', '${route}', 'GET', ${status}, ${count}, ${sum}, ${arrayLiteral(b)})
    ON CONFLICT (tenant_id, api_service_id, route, method, status_code, bucket_start) DO UPDATE SET
      request_count = m.request_count + EXCLUDED.request_count,
      duration_sum = m.duration_sum + EXCLUDED.duration_sum,
      duration_buckets = api_metric_add_buckets(m.duration_buckets, EXCLUDED.duration_buckets)`;
}

const tables = ["api_metric_1m", "api_metric_1h", "api_metric_1d"];

async function partitionNames(): Promise<string[]> {
  const rows = await sql<{ child: string }[]>`
    SELECT c.relname AS child
    FROM pg_inherits i
    JOIN pg_class p ON p.oid = i.inhparent
    JOIN pg_class c ON c.oid = i.inhrelid
    WHERE p.relname IN ('api_metric_1m', 'api_metric_1h', 'api_metric_1d')`;
  return rows.map((r) => r.child);
}

// Partições que existiam antes do teste. O teste da default cria partições
// futuras; sem removê-las, a segunda execução no mesmo banco acharia o mês
// distante já coberto e o ponto nunca passaria pela default.
let baseline: string[] = [];

beforeAll(async () => {
  await runMigrations();
  baseline = await partitionNames();
});

afterAll(async () => {
  for (const t of tables) {
    await sql.unsafe(`DELETE FROM ${t} WHERE tenant_id = $1`, [tenantId]);
  }
  for (const name of await partitionNames()) {
    if (baseline.includes(name)) continue;
    // Só as criadas por este teste, e só se já estiverem vazias.
    const rows = await sql.unsafe(`SELECT count(*)::int AS n FROM ${name}`);
    if (rows[0]!.n === 0) await sql.unsafe(`DROP TABLE ${name}`);
  }
  await sql.end();
});

describe("tabelas e partições", () => {
  it("cria as três tabelas particionadas, com a default e os próximos meses", async () => {
    const rows = await sql<{ parent: string; child: string }[]>`
      SELECT p.relname AS parent, c.relname AS child
      FROM pg_inherits i
      JOIN pg_class p ON p.oid = i.inhparent
      JOIN pg_class c ON c.oid = i.inhrelid
      WHERE p.relname IN ('api_metric_1m', 'api_metric_1h', 'api_metric_1d')`;
    const now = new Date();
    const month = (offset: number) => {
      const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + offset, 1));
      return `${d.getUTCFullYear()}${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
    };
    for (const t of ["api_metric_1m", "api_metric_1h", "api_metric_1d"]) {
      const children = rows.filter((r) => r.parent === t).map((r) => r.child);
      expect(children).toContain(`${t}_default`);
      for (const offset of [0, 1, 2]) expect(children).toContain(`${t}_p${month(offset)}`);
    }
  });

  it("guarda na default um ponto sem partição e o move quando ela é criada", async () => {
    // Um mês depois da última partição que já existe, para o ponto cair na
    // default mesmo que outra execução tenha criado partições à frente.
    const now = new Date();
    const lastPartition = baseline
      .filter((n) => /^api_metric_1m_p\d{6}$/.test(n))
      .map((n) => n.slice(-6))
      .sort()
      .at(-1)!;
    const lastMonths = Number(lastPartition.slice(0, 4)) * 12 + Number(lastPartition.slice(4)) - 1;
    const nowMonths = now.getUTCFullYear() * 12 + now.getUTCMonth();
    const ahead = lastMonths - nowMonths + 1;
    const far = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + ahead, 1));
    const at = far.toISOString();
    await sql.unsafe(upsert("api_metric_1m", at, "/atrasado", 200, 1, 0.01, buckets({ 1: 1 })));

    const where = `tenant_id = '${tenantId}' AND route = '/atrasado'`;
    expect(await sql.unsafe(`SELECT 1 FROM api_metric_1m_default WHERE ${where}`)).toHaveLength(1);

    await sql`SELECT api_metric_ensure_partitions(${ahead})`;
    await sql`SELECT api_metric_ensure_partitions(${ahead})`; // idempotente

    expect(await sql.unsafe(`SELECT 1 FROM api_metric_1m_default WHERE ${where}`)).toHaveLength(0);
    const [moved] = await sql.unsafe(`SELECT tableoid::regclass::text AS part FROM api_metric_1m WHERE ${where}`);
    expect(moved!.part).toMatch(/^api_metric_1m_p\d{6}$/);
  });

  it("recusa buckets fora do formato e rota vazia", async () => {
    const at = new Date().toISOString();
    await expect(sql.unsafe(upsert("api_metric_1m", at, "/x", 200, 1, 0.01, [1, 2, 3]))).rejects.toThrow();
    await expect(sql.unsafe(upsert("api_metric_1m", at, "", 200, 1, 0.01, buckets({})))).rejects.toThrow();
  });
});

describe("upsert e percentis", () => {
  const minute = "2026-11-14T10:00:00Z";

  it("soma contagem, duração e buckets do mesmo balde", async () => {
    await sql.unsafe(upsert("api_metric_1m", minute, "/ledgers/{id}", 200, 50, 1, buckets({ 2: 50 })));
    await sql.unsafe(upsert("api_metric_1m", minute, "/ledgers/{id}", 200, 50, 300, buckets({ 11: 50 })));
    const [row] = await sql<{ request_count: string; duration_sum: number; duration_buckets: string[] }[]>`
      SELECT request_count, duration_sum, duration_buckets FROM api_metric_1m
      WHERE tenant_id = ${tenantId} AND route = '/ledgers/{id}' AND bucket_start = ${minute}`;
    expect(Number(row!.request_count)).toBe(100);
    expect(row!.duration_sum).toBe(301);
    expect(row!.duration_buckets.map(Number)).toEqual(buckets({ 2: 50, 11: 50 }));
  });

  it("calcula p50 e p95 por rota e período", async () => {
    // Mais uma linha da mesma rota em outro minuto e outra rota rápida.
    await sql.unsafe(upsert("api_metric_1m", "2026-11-14T10:01:00Z", "/ledgers/{id}", 500, 0, 0, buckets({})));
    await sql.unsafe(upsert("api_metric_1m", "2026-11-14T10:01:00Z", "/health", 200, 10, 0.02, buckets({ 0: 10 })));

    const rows = await sql<{ route: string; requests: string; p50: number; p95: number }[]>`
      SELECT route, sum(request_count) AS requests,
             api_metric_quantile(api_metric_sum_buckets(duration_buckets), 0.5) AS p50,
             api_metric_quantile(api_metric_sum_buckets(duration_buckets), 0.95) AS p95
      FROM api_metric_1m
      WHERE tenant_id = ${tenantId} AND bucket_start >= '2026-11-14T10:00:00Z' AND bucket_start < '2026-11-14T11:00:00Z'
      GROUP BY route ORDER BY route`;

    expect(rows.map((r) => r.route)).toEqual(["/health", "/ledgers/{id}"]);
    // /ledgers: 50 no terceiro bucket, (0.01, 0.025], e 50 no décimo segundo,
    // (2.5, 5]. p50 fecha o terceiro (0.025); p95 cai a 45/50 do décimo
    // segundo: 2.5 + 2.5 * 0.9 = 4.75.
    const ledgers = rows[1]!;
    expect(Number(ledgers.requests)).toBe(100);
    expect(ledgers.p50).toBeCloseTo(0.025, 9);
    expect(ledgers.p95).toBeCloseTo(4.75, 9);
    // /health: tudo no primeiro bucket [0, 0.005].
    expect(rows[0]!.p50).toBeCloseTo(0.0025, 9);
  });

  it("trata os extremos do percentil", async () => {
    const q = async (b: number[], p: number) =>
      (await sql.unsafe(`SELECT api_metric_quantile(${arrayLiteral(b)}, ${p}) AS v`))[0]!.v as number | null;
    expect(await q(buckets({}), 0.5)).toBeNull();
    expect(await q(buckets({ 14: 3 }), 0.5)).toBe(10); // acima de 10 s
    expect(await q(buckets({ 4: 1 }), 0)).toBeCloseTo(0.05, 9);
    expect(await q(buckets({ 4: 1 }), 1)).toBeCloseTo(0.075, 9);
    expect(await q(buckets({ 4: 1 }), 1.5)).toBeNull();
  });
});

describe("permissões", () => {
  const at = "2026-11-14T10:00:00Z";

  it("o coletor grava e acumula no balde de 1 minuto", async () => {
    await asRole("notify_collector", upsert("api_metric_1m", at, "/coletor", 201, 1, 0.1, buckets({ 5: 1 })));
    await asRole("notify_collector", upsert("api_metric_1m", at, "/ledgers/{id}", 200, 1, 0.1, buckets({ 5: 1 })));
  });

  it("o coletor não apaga, não grava nos consolidados e não mexe em partições", async () => {
    await expect(asRole("notify_collector", `DELETE FROM api_metric_1m WHERE tenant_id = '${tenantId}'`)).rejects.toThrow(
      /permission denied/,
    );
    await expect(
      asRole("notify_collector", upsert("api_metric_1h", at, "/x", 200, 1, 0.1, buckets({ 5: 1 }))),
    ).rejects.toThrow(/permission denied/);
    await expect(asRole("notify_collector", "SELECT api_metric_ensure_partitions(1)")).rejects.toThrow(
      /permission denied/,
    );
  });

  it("o gateway web não enxerga as métricas", async () => {
    await expect(asRole("notify_ingest", "SELECT 1 FROM api_metric_1m LIMIT 1")).rejects.toThrow(/permission denied/);
  });
});
