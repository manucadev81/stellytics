import type { Metadata } from "next";
import { requireSession } from "@/lib/session";
import { DeleteAccountForm } from "./delete-account-form";

export const metadata: Metadata = { title: "Account" };

// Configurações da conta (#154). Hoje só o nome e o e-mail (a retificação de
// e-mail por tela fica para a issue de pedidos de titulares) e a exclusão da
// própria conta.
export default async function AccountPage() {
  const { user } = await requireSession();

  return (
    <section>
      <h1 className="text-2xl font-semibold">Account</h1>
      <dl className="mt-4 max-w-3xl divide-y divide-zinc-200 border-y border-zinc-200 text-sm dark:divide-zinc-800 dark:border-zinc-800">
        <div className="flex justify-between py-2">
          <dt className="text-zinc-500">Email</dt>
          <dd>{user.email}</dd>
        </div>
        <div className="flex justify-between py-2">
          <dt className="text-zinc-500">Name</dt>
          <dd>{user.name}</dd>
        </div>
        <div className="flex justify-between py-2">
          <dt className="text-zinc-500">Role</dt>
          <dd className="capitalize">{user.role}</dd>
        </div>
      </dl>

      <h2 className="mt-8 text-lg font-semibold text-red-700 dark:text-red-400">Delete account</h2>
      <DeleteAccountForm email={user.email} />
    </section>
  );
}
