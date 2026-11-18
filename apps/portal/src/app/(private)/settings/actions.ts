"use server";

import { redirect } from "next/navigation";
import { refresh } from "next/cache";
import { requireSession } from "@/lib/session";
import { deleteOwnAccount } from "@/lib/data/erasure";

// Exclusão da própria conta (#154). A confirmação é o e-mail digitado; a
// sessão sai junto (cascade), então a resposta vai para /sign-in.

export type AccountActionState = { status: "idle" } | { status: "error"; message: string };

export async function deleteAccountAction(_prev: AccountActionState, form: FormData): Promise<AccountActionState> {
  const session = await requireSession();
  const confirm = String(form.get("confirm") ?? "").trim().toLowerCase();
  if (confirm !== session.user.email.toLowerCase()) {
    return { status: "error", message: "Type your account email to confirm deletion." };
  }
  const result = await deleteOwnAccount(session.user.id);
  if (result === null) {
    // A conta já não existe (por exemplo, clique duplo): tratar como concluído.
    redirect("/sign-in");
  }
  refresh();
  redirect("/sign-in?deleted=1");
}
