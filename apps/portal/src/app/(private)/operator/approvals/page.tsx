import type { Metadata } from "next";
import Link from "next/link";
import { requireOperatorContext } from "@/lib/access";
import { env } from "@/lib/env";
import { listPendingTenants, REASON_MAX } from "@/lib/data/operator/tenants";
import { ApprovalForm } from "../tenants/[tenantId]/approval-form";

export const metadata: Metadata = { title: "Sign-up approvals" };

const dateTimeFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" });

// Fila de aprovação de cadastros (#49, ADR 0007): projetos `pending`, do mais
// antigo para o mais novo, com aprovar e rejeitar em cada um. A fila fica vazia
// quando SIGNUP_APPROVAL está desligado. Quem não é operador recebe 404.
export default async function ApprovalsPage() {
  const ctx = await requireOperatorContext();
  const pending = await listPendingTenants(ctx);

  return (
    <section>
      <h1 className="text-2xl font-semibold">Sign-up approvals</h1>
      <p className="mt-2 text-zinc-500">
        {pending.length === 0
          ? "No projects are waiting for approval."
          : `${pending.length} project${pending.length === 1 ? " is" : "s are"} waiting for approval.`}
      </p>
      {pending.length > 0 && !env.signupApproval && (
        <p className="mt-2 text-sm text-zinc-500">
          Sign-up approval is currently turned off, so new projects are active right away. The projects below signed up
          while it was on and stay pending until you approve or reject them.
        </p>
      )}
      <ul className="mt-6 space-y-8">
        {pending.map((t) => (
          <li key={t.id} className="border-t border-zinc-200 pt-4 dark:border-zinc-800">
            <Link href={`/operator/tenants/${t.id}`} className="font-medium hover:underline">
              {t.name}
            </Link>
            <p className="mt-1 text-sm text-zinc-500">
              {t.ownerEmail ?? "No owner"} · signed up {dateTimeFormat.format(t.createdAt)} UTC
            </p>
            <ApprovalForm tenantId={t.id} reasonMax={REASON_MAX} />
          </li>
        ))}
      </ul>
    </section>
  );
}
