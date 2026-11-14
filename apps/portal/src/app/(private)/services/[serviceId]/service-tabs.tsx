import Link from "next/link";

// Abas de um serviço de API: o painel (#37) e as chaves de ingestão (#59).
export function ServiceTabs({ serviceId, current }: { serviceId: string; current: "overview" | "keys" }) {
  const tabs = [
    { key: "overview", label: "Overview", href: `/services/${serviceId}` },
    { key: "keys", label: "Ingest keys", href: `/services/${serviceId}/keys` },
  ] as const;
  return (
    <nav aria-label="Service" className="mt-4 flex gap-4 border-b border-zinc-200 text-sm dark:border-zinc-800">
      {tabs.map((t) => (
        <Link
          key={t.key}
          href={t.href}
          aria-current={t.key === current ? "page" : undefined}
          className={
            t.key === current
              ? "-mb-px border-b-2 border-zinc-900 pb-2 font-medium dark:border-zinc-100"
              : "pb-2 text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          }
        >
          {t.label}
        </Link>
      ))}
    </nav>
  );
}
