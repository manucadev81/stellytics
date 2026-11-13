import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { requireOperatorContext } from "@/lib/access";
import { getTenantDetail } from "@/lib/data/operator/tenants";

export const metadata: Metadata = { title: "Project details" };

const dateFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

// Detalhe de um projeto para o operador SCF (#19), só leitura. Das chaves de
// ingestão aparecem só o prefixo e as datas.
export default async function OperatorTenantPage({ params }: { params: Promise<{ tenantId: string }> }) {
  const { tenantId } = await params;
  const ctx = await requireOperatorContext();
  const detail = await getTenantDetail(ctx, tenantId);
  if (!detail) notFound();
  const { tenant, members, sites, services } = detail;

  return (
    <section>
      <Link href="/operator" className="text-sm text-zinc-500 hover:underline">
        ← SCF overview
      </Link>
      <h1 className="mt-2 text-2xl font-semibold">{tenant.name}</h1>
      <p className="mt-1 text-sm text-zinc-500">
        <span className="font-mono">{tenant.slug}</span> · created {dateFormat.format(tenant.createdAt)} ·{" "}
        <span className="capitalize">{tenant.status}</span>
        {tenant.status !== "active" && ` since ${dateFormat.format(tenant.statusChangedAt)}`}
      </p>
      {tenant.statusReason && <p className="mt-1 text-sm text-zinc-500">Reason: {tenant.statusReason}</p>}

      <h2 className="mt-8 text-lg font-semibold">Members</h2>
      <ul className="mt-2 divide-y divide-zinc-200 border-y border-zinc-200 text-sm dark:divide-zinc-800 dark:border-zinc-800">
        {members.map((m) => (
          <li key={m.email} className="flex flex-wrap justify-between gap-2 py-2">
            <span>
              {m.name} <span className="text-zinc-500">{m.email}</span>
            </span>
            <span className="capitalize text-zinc-500">{m.role}</span>
          </li>
        ))}
      </ul>

      <h2 className="mt-8 text-lg font-semibold">Websites</h2>
      <ul className="mt-2 divide-y divide-zinc-200 border-y border-zinc-200 text-sm dark:divide-zinc-800 dark:border-zinc-800">
        {sites.length === 0 && <li className="py-2 text-zinc-500">No websites.</li>}
        {sites.map((s) => (
          <li key={s.id} className="flex flex-wrap justify-between gap-2 py-2">
            <span>
              {s.name} <span className="text-zinc-500">{s.domain}</span>
            </span>
            <span className="text-zinc-500">{s.archivedAt ? "Archived" : dateFormat.format(s.createdAt)}</span>
          </li>
        ))}
      </ul>

      <h2 className="mt-8 text-lg font-semibold">API services</h2>
      {services.length === 0 && <p className="mt-2 text-sm text-zinc-500">No API services.</p>}
      {services.map((service) => (
        <div key={service.id} className="mt-4">
          <p className="font-medium">
            {service.name}
            {service.archivedAt && <span className="ml-2 text-sm text-zinc-500">archived</span>}
          </p>
          <table className="mt-2 w-full text-left text-sm">
            <thead className="border-b border-zinc-200 text-zinc-500 dark:border-zinc-800">
              <tr>
                <th className="py-1 font-medium">Key</th>
                <th className="py-1 font-medium">Created</th>
                <th className="py-1 font-medium">Last used</th>
                <th className="py-1 font-medium">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-200 dark:divide-zinc-800">
              {service.keys.map((k) => (
                <tr key={k.prefix}>
                  <td className="py-1 font-mono">ntf_{k.prefix}_…</td>
                  <td className="py-1">{dateFormat.format(k.createdAt)}</td>
                  <td className="py-1">{k.lastUsedOn ?? "Never"}</td>
                  <td className="py-1">{k.revokedAt ? "Revoked" : "Active"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </section>
  );
}
