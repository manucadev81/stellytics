import "server-only";
import type { TenantContext } from "../access";
import { sql } from "../db";
import type { Period } from "../period";
import { getApiService } from "./api-services";

// Painel de API do tenant (#37): volume, status e latência p50/p95 por
// endpoint de um serviço num período. Lê as tabelas do spec do E5 (seção 4)
// direto do banco notify, sempre filtrando pelo tenant do contexto e pelo
// serviço já conferido (ADR 0003): serviço de outro tenant devolve nulo sem
// nenhuma leitura de métricas.

// Endpoints na tabela, os de mais requisições primeiro. O coletor limita a 500
// rotas por serviço e dia (D4 do spec do E5), então o resto é cauda.
export const ENDPOINT_LIMIT = 100;

export type ApiCounts = {
  requests: number;
  clientErrors: number; // 4xx
  serverErrors: number; // 5xx
  // Percentis em segundos, nulos sem requisições. 10 quer dizer "10 s ou mais"
  // (o último bucket do histograma não tem limite superior).
  p50: number | null;
  p95: number | null;
};

export type ApiEndpoint = ApiCounts & { method: string; route: string };

export type ApiServiceDashboard = {
  totals: ApiCounts;
  // Um ponto por balde do período, inclusive os vazios.
  series: (ApiCounts & { at: Date })[];
  endpoints: ApiEndpoint[];
  endpointCount: number;
  statuses: { statusCode: number; requests: number }[];
};

const HOUR = 3_600_000;

function floorHour(d: Date): Date {
  return new Date(Math.floor(d.getTime() / HOUR) * HOUR);
}

function floorDay(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
}

// De qual tabela vem cada trecho do período. O job da #34 consolida a cada
// 15 minutos as horas que terminaram há pelo menos 5 minutos, e o dia a partir
// das horas. Com uma hora de folga, o que começou antes de `hourCut` já está
// em api_metric_1h, e os dias antes de `dayCut`, em api_metric_1d. O resto
// (no máximo as duas últimas horas) vem de api_metric_1m. Os trechos não se
// sobrepõem, então nada conta duas vezes.
export function metricSources(period: Pick<Period, "unit" | "startAt" | "endAt">) {
  const hourCut = new Date(floorHour(period.endAt).getTime() - HOUR);
  const dayCut = floorDay(hourCut);
  const { startAt: start, endAt: end } = period;
  const max = (a: Date, b: Date) => (a > b ? a : b);
  const min = (a: Date, b: Date) => (a < b ? a : b);
  // Período por hora não pode usar o balde de um dia.
  const daily = period.unit !== "hour";
  return {
    day: daily ? { from: start, to: min(dayCut, end) } : null,
    hour: { from: daily ? max(start, dayCut) : start, to: min(hourCut, end) },
    minute: { from: max(start, hourCut), to: end },
  };
}

type Row = {
  g_bucket: number;
  g_route: number;
  g_status: number;
  bucket: Date | string | null; // o driver pode devolver texto para a expressão
  route: string | null;
  method: string | null;
  status_code: number | null;
  requests: string;
  client_errors: string;
  server_errors: string;
  p50: number | null;
  p95: number | null;
};

function counts(r: Row): ApiCounts {
  return {
    requests: Number(r.requests),
    clientErrors: Number(r.client_errors),
    serverErrors: Number(r.server_errors),
    p50: r.p50,
    p95: r.p95,
  };
}

const EMPTY: ApiCounts = { requests: 0, clientErrors: 0, serverErrors: 0, p50: null, p95: null };

// Nulo quando o serviço não é do tenant do contexto (ou está arquivado).
export async function getServiceDashboard(
  ctx: TenantContext,
  serviceId: string,
  period: Period,
): Promise<ApiServiceDashboard | null> {
  const service = await getApiService(ctx, serviceId);
  if (!service) return null;
  const src = metricSources(period);
  // Trecho vazio (from >= to) não devolve linha; o de dias some no período por hora.
  const day = src.day ?? { from: period.startAt, to: period.startAt };

  // Uma leitura só: GROUPING SETS dá a série, os endpoints, os status e o total.
  // As datas vão como texto ISO com cast: o driver não serializa Date aqui.
  const rows = await sql<Row[]>`
    WITH r AS (
      SELECT bucket_start, route, method, status_code, request_count, duration_buckets FROM api_metric_1d
      WHERE tenant_id = ${ctx.tenantId} AND api_service_id = ${service.id}
        AND bucket_start >= ${day.from.toISOString()}::timestamptz AND bucket_start < ${day.to.toISOString()}::timestamptz
      UNION ALL
      SELECT bucket_start, route, method, status_code, request_count, duration_buckets FROM api_metric_1h
      WHERE tenant_id = ${ctx.tenantId} AND api_service_id = ${service.id}
        AND bucket_start >= ${src.hour.from.toISOString()}::timestamptz AND bucket_start < ${src.hour.to.toISOString()}::timestamptz
      UNION ALL
      SELECT bucket_start, route, method, status_code, request_count, duration_buckets FROM api_metric_1m
      WHERE tenant_id = ${ctx.tenantId} AND api_service_id = ${service.id}
        AND bucket_start >= ${src.minute.from.toISOString()}::timestamptz AND bucket_start < ${src.minute.to.toISOString()}::timestamptz
    )
    SELECT
      GROUPING(bucket)::int AS g_bucket, GROUPING(route, method)::int AS g_route, GROUPING(status_code)::int AS g_status,
      bucket, route, method, status_code,
      sum(request_count) AS requests,
      coalesce(sum(request_count) FILTER (WHERE status_code BETWEEN 400 AND 499), 0) AS client_errors,
      coalesce(sum(request_count) FILTER (WHERE status_code >= 500), 0) AS server_errors,
      api_metric_quantile(api_metric_sum_buckets(duration_buckets), 0.5) AS p50,
      api_metric_quantile(api_metric_sum_buckets(duration_buckets), 0.95) AS p95
    FROM (SELECT date_trunc(${period.unit}::text, bucket_start, 'UTC') AS bucket, * FROM r) b
    GROUP BY GROUPING SETS ((bucket), (route, method), (status_code), ())
  `;

  let totals = EMPTY;
  const byBucket = new Map<number, ApiCounts>();
  const endpoints: ApiEndpoint[] = [];
  const statuses: ApiServiceDashboard["statuses"] = [];
  for (const r of rows) {
    if (r.g_bucket === 0) byBucket.set(new Date(r.bucket!).getTime(), counts(r));
    else if (r.g_route === 0) endpoints.push({ method: r.method!, route: r.route!, ...counts(r) });
    else if (r.g_status === 0) statuses.push({ statusCode: r.status_code!, requests: Number(r.requests) });
    else totals = counts(r);
  }
  const byRequests = (a: { requests: number }, b: { requests: number }) => b.requests - a.requests;
  endpoints.sort((a, b) => byRequests(a, b) || a.route.localeCompare(b.route) || a.method.localeCompare(b.method));
  statuses.sort((a, b) => byRequests(a, b) || a.statusCode - b.statusCode);

  return {
    totals,
    series: period.buckets.map((at) => ({ at, ...(byBucket.get(at.getTime()) ?? EMPTY) })),
    endpoints: endpoints.slice(0, ENDPOINT_LIMIT),
    endpointCount: endpoints.length,
    statuses,
  };
}
