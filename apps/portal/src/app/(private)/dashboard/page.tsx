import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getTenantContext } from "@/lib/access";
import { listSites } from "@/lib/data/sites";
import { requireSession } from "@/lib/session";

export const metadata: Metadata = { title: "Project" };

// Área do tenant. Os painéis web (E4) e de API (E6) entram aqui. As rotas do
// tenant não levam o ID do tenant na URL: ele vem da sessão (spec do E2, seção 4).
export default async function DashboardPage() {
  const { user } = await requireSession();
  // /dashboard é o destino padrão depois do login; o operador não tem projeto (D2).
  if (user.role === "operator") redirect("/operator");

  const ctx = await getTenantContext();
  if (!ctx) {
    // A #17 troca isto pelo redirecionamento à criação de projeto.
    return (
      <section>
        <h1 className="text-2xl font-semibold">Project</h1>
        <p className="mt-2 text-zinc-500">You don&apos;t have a project yet.</p>
      </section>
    );
  }

  if (ctx.tenantStatus === "removed") {
    return (
      <section>
        <h1 className="text-2xl font-semibold">Project</h1>
        <p className="mt-2 text-zinc-500">This project was removed by the SCF operators.</p>
      </section>
    );
  }

  const sites = await listSites(ctx);

  return (
    <section>
      <h1 className="text-2xl font-semibold">Project</h1>
      <p className="mt-2 text-zinc-500">
        Signed in as {user.name}. Your web and API analytics will appear here.
      </p>
      <p className="mt-4 text-sm text-zinc-500">
        {sites.length === 0 ? "No websites yet." : `${sites.length} website(s) registered.`}
      </p>
    </section>
  );
}
