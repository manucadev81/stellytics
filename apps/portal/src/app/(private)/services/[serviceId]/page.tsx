import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { requireTenantContext } from "@/lib/access";
import { getApiService } from "@/lib/data/api-services";
import { type ApiCounts, ENDPOINT_LIMIT, getServiceDashboard } from "@/lib/data/api-metrics";
import { getProject } from "@/lib/data/tenants";
import { bucketLabel, formatCount, formatLatency, formatShare, statusLabel } from "@/lib/format";
import { ColumnChart } from "@/components/column-chart";
import { PeriodPicker } from "@/components/period-picker";
import { parsePeriodKey, resolvePeriod } from "@/lib/period";
import { LatencyChart } from "./latency-chart";
import { ServiceTabs } from "./service-tabs";

export const metadata: Metadata = { title: "API analytics" };

const card = "rounded-lg border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900";
const full = new Intl.NumberFormat("en");
const plural = (n: number, one: string, many: string) => `${full.format(n)} ${n === 1 ? one : many}`;

// Painel de API do tenant (#37): volume, distribuição de status e latência
// p50/p95 por endpoint de um serviço no período escolhido. Serviço de outro
// tenant (ou arquivado) responde 404 antes de qualquer leitura de métricas;
// projeto removido vê só o aviso (spec do E2, seção 6).
export default async function ServicePage({
  params,
  searchParams,
}: {
  params: Promise<{ serviceId: string }>;
  searchParams?: Promise<{ period?: string | string[] }>;
}) {
  const [{ serviceId }, query] = await Promise.all([params, searchParams ?? Promise.resolve({})]);
  const ctx = await requireTenantContext();
  const service = await getApiService(ctx, serviceId);
  if (!service) notFound();

  const header = (
    <>
      <Link href="/services" className="text-sm text-zinc-500 hover:underline">
        ← API services
      </Link>
      <h1 className="mt-2 text-2xl font-semibold">{service.name}</h1>
      <ServiceTabs serviceId={service.id} current="overview" />
    </>
  );

  if (ctx.tenantStatus === "removed") {
    const project = await getProject(ctx);
    return (
      <section>
        {header}
        <p className="mt-6 text-zinc-500">This project was removed by the SCF operators.</p>
        {project.statusReason && <p className="mt-2 text-sm text-zinc-500">Reason: {project.statusReason}</p>}
      </section>
    );
  }

  const period = resolvePeriod(parsePeriodKey((query as { period?: unknown }).period));
  const data = await getServiceDashboard(ctx, service.id, period);
  if (!data) notFound();

  const { totals, series, endpoints, endpointCount, statuses } = data;
  const unitName = period.unit === "hour" ? "Hour" : period.unit === "day" ? "Day" : "Month";
  const points = series.map((p) => ({ ...p, label: bucketLabel(period.unit, p.at), longLabel: bucketLabel(period.unit, p.at, true) }));

  return (
    <section>
      {header}
      <PeriodPicker current={period.key} href={(key) => `/services/${service.id}?period=${key}`} />

      {totals.requests === 0 && (
        <p className="mt-6 rounded-lg border border-dashed border-zinc-300 p-4 text-sm text-zinc-500 dark:border-zinc-700">
          No requests in this period yet. Send OpenTelemetry HTTP server metrics or traces with an{" "}
          <Link href={`/services/${service.id}/keys`} className="underline">
            ingest key
          </Link>{" "}
          to see them here. New data shows up within a minute.
        </p>
      )}

      <div className="mt-6 grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatTile label="Requests" value={formatCount(totals.requests)} />
        <StatTile
          label="Server errors (5xx)"
          value={formatShare(totals.serverErrors, totals.requests)}
          note={totals.requests > 0 ? plural(totals.serverErrors, "request", "requests") : undefined}
        />
        <StatTile label="Median latency (p50)" value={formatLatency(totals.p50)} />
        <StatTile label="p95 latency" value={formatLatency(totals.p95)} />
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-2">
        <div className={card}>
          <h2 className="font-medium">Requests</h2>
          <p className="text-sm text-zinc-500">
            {period.label}, per {period.unit} (UTC), including the current {period.unit}
          </p>
          <div className="mt-4">
            <ColumnChart
              label="Requests per period"
              points={points.map((p) => ({
                label: p.label,
                longLabel: p.longLabel,
                value: p.requests,
                description: `${p.longLabel}: ${plural(p.requests, "request", "requests")}, ${plural(p.serverErrors, "server error", "server errors")}`,
                tooltip: [
                  { value: full.format(p.requests), unit: p.requests === 1 ? "request" : "requests" },
                  { value: full.format(p.serverErrors), unit: "5xx" },
                  { value: full.format(p.clientErrors), unit: "4xx" },
                ],
              }))}
            />
          </div>
        </div>
        <div className={card}>
          <h2 className="font-medium">Latency</h2>
          <p className="text-sm text-zinc-500">
            {period.label}, per {period.unit} (UTC). Gaps are periods without requests.
          </p>
          <div className="mt-4">
            <LatencyChart points={points.map(({ label, longLabel, p50, p95 }) => ({ label, longLabel, p50, p95 }))} />
          </div>
        </div>
      </div>
      <details className={`mt-4 ${card} text-sm`}>
        <summary className="cursor-pointer text-zinc-500">Show charts as a table</summary>
        <div className="mt-2 max-h-72 overflow-auto">
          <table className="w-full text-left tabular-nums">
            <thead className="text-zinc-500">
              <tr>
                <th className="py-1 font-medium">{unitName}</th>
                <th className="py-1 text-right font-medium">Requests</th>
                <th className="py-1 text-right font-medium">4xx</th>
                <th className="py-1 text-right font-medium">5xx</th>
                <th className="py-1 text-right font-medium">p50</th>
                <th className="py-1 text-right font-medium">p95</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-200 dark:divide-zinc-800">
              {points.map((p) => (
                <tr key={p.longLabel}>
                  <td className="whitespace-nowrap py-1 pr-3">{p.longLabel}</td>
                  <td className="py-1 text-right">{full.format(p.requests)}</td>
                  <td className="py-1 text-right">{full.format(p.clientErrors)}</td>
                  <td className="py-1 text-right">{full.format(p.serverErrors)}</td>
                  <td className="py-1 text-right">{formatLatency(p.p50)}</td>
                  <td className="py-1 text-right">{formatLatency(p.p95)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>

      <div className="mt-4 grid gap-4 lg:grid-cols-[2fr_1fr]">
        <div className={`${card} min-w-0`}>
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h2 className="font-medium">Endpoints</h2>
            <span className="text-xs text-zinc-500">
              {endpointCount > ENDPOINT_LIMIT
                ? `Top ${ENDPOINT_LIMIT} of ${full.format(endpointCount)} by requests`
                : `${plural(endpointCount, "endpoint", "endpoints")}, by requests`}
            </span>
          </div>
          {endpoints.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">No data in this period.</p>
          ) : (
            <div className="mt-3 overflow-x-auto">
              <table className="w-full text-left text-sm tabular-nums">
                <thead className="border-b border-zinc-200 text-zinc-500 dark:border-zinc-800">
                  <tr>
                    <th className="py-1.5 pr-3 font-medium">Endpoint</th>
                    <th className="py-1.5 pl-3 text-right font-medium">Requests</th>
                    <th className="py-1.5 pl-3 text-right font-medium">4xx</th>
                    <th className="py-1.5 pl-3 text-right font-medium">5xx</th>
                    <th className="py-1.5 pl-3 text-right font-medium">p50</th>
                    <th className="py-1.5 pl-3 text-right font-medium">p95</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-200 dark:divide-zinc-800">
                  {endpoints.map((e) => (
                    <tr key={`${e.method} ${e.route}`}>
                      <td className="min-w-44 max-w-80 py-1.5 pr-3">
                        <span className="block font-mono text-xs text-zinc-500 sm:mr-2 sm:inline-block sm:w-14">{e.method}</span>
                        <span className="break-all font-mono text-xs">{e.route}</span>
                      </td>
                      <td className="py-1.5 pl-3 text-right">{formatCount(e.requests)}</td>
                      <td className="py-1.5 pl-3 text-right">{formatShare(e.clientErrors, e.requests)}</td>
                      <td className="py-1.5 pl-3 text-right">{formatShare(e.serverErrors, e.requests)}</td>
                      <td className="whitespace-nowrap py-1.5 pl-3 text-right">{formatLatency(e.p50)}</td>
                      <td className="whitespace-nowrap py-1.5 pl-3 text-right">{formatLatency(e.p95)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="mt-3 text-xs text-zinc-500">
            Paths are grouped: IDs, Stellar addresses, UUIDs and hashes become placeholders such as {"{id}"}.
          </p>
        </div>

        <StatusCodes statuses={statuses} total={totals.requests} />
      </div>
    </section>
  );
}

function StatTile({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className={card}>
      <p className="text-sm text-zinc-500">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
      {note && <p className="mt-1 text-xs text-zinc-500">{note}</p>}
    </div>
  );
}

// Distribuição de status: código, barra e número por linha, como as listas do
// painel web. A barra usa a cor da série; a classe (2xx, 4xx...) vem escrita.
function StatusCodes({ statuses, total }: { statuses: { statusCode: number; requests: number }[]; total: ApiCounts["requests"] }) {
  const max = Math.max(1, ...statuses.map((s) => s.requests));
  return (
    <div className={`${card} [--series:#2a78d6] dark:[--series:#3987e5]`}>
      <div className="flex items-baseline justify-between">
        <h2 className="font-medium">Status codes</h2>
        <span className="text-xs text-zinc-500">Requests</span>
      </div>
      {statuses.length === 0 ? (
        <p className="mt-3 text-sm text-zinc-500">No data in this period.</p>
      ) : (
        <ol className="mt-3 space-y-2 text-sm">
          {statuses.map((s) => (
            <li key={s.statusCode}>
              <div className="flex items-baseline justify-between gap-3">
                <span className="truncate">{statusLabel(s.statusCode)}</span>
                <span className="whitespace-nowrap tabular-nums">
                  {formatCount(s.requests)} <span className="text-zinc-500">· {formatShare(s.requests, total)}</span>
                </span>
              </div>
              <div className="mt-1 h-1.5 rounded-r-[4px] bg-[var(--series)]" style={{ width: `${(s.requests / max) * 100}%` }} />
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
