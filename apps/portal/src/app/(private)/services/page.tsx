import type { Metadata } from "next";
import Link from "next/link";
import { requireTenantContext } from "@/lib/access";
import { listApiServices } from "@/lib/data/api-services";
import { CreateServiceForm } from "./key-forms";

export const metadata: Metadata = { title: "API services" };

// Serviços de API do tenant (#59). Cada serviço recebe a primeira chave de
// ingestão na criação.
export default async function ServicesPage() {
  const ctx = await requireTenantContext();
  const services = await listApiServices(ctx);
  const canWrite = ctx.tenantStatus !== "removed";

  return (
    <section>
      <h1 className="text-2xl font-semibold">API services</h1>
      <p className="mt-2 text-zinc-500">
        Each service sends OpenTelemetry metrics with its own ingest key.
      </p>

      {canWrite ? (
        <div className="mt-6">
          <CreateServiceForm />
        </div>
      ) : (
        <p className="mt-6 text-sm text-zinc-500">This project was removed; new services can&apos;t be added.</p>
      )}

      <ul className="mt-8 divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {services.length === 0 && <li className="py-3 text-sm text-zinc-500">No services yet.</li>}
        {services.map((service) => (
          <li key={service.id} className="py-3">
            <Link href={`/services/${service.id}`} className="font-medium hover:underline">
              {service.name}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
