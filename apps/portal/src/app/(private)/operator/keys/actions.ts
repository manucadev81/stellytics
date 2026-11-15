"use server";

import { refresh } from "next/cache";
import { notFound } from "next/navigation";
import { requireOperatorContext } from "@/lib/access";
import { createReadKey, ReadKeyError, revokeReadKey } from "@/lib/data/operator/api-keys";

// Server actions das chaves de leitura do SCF (#48). São endpoints públicos:
// cada uma exige operador na sessão (quem não é recebe 404). A chave completa
// volta só no estado da ação, uma única vez, e nunca vai para log.

export type ReadKeyState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; key: string; prefix: string };

export async function createReadKeyAction(_prev: ReadKeyState, form: FormData): Promise<ReadKeyState> {
  const ctx = await requireOperatorContext();
  try {
    const created = await createReadKey(ctx, { name: String(form.get("name") ?? "") });
    refresh();
    return { status: "created", key: created.key, prefix: created.summary.prefix };
  } catch (error) {
    if (error instanceof ReadKeyError) return { status: "error", message: error.message };
    // Erro inesperado: a mensagem não chega ao cliente e não leva a chave.
    console.error("Falha ao criar chave de leitura", (error as Error).name);
    return { status: "error", message: "Something went wrong. Try again." };
  }
}

export async function revokeReadKeyAction(keyId: string): Promise<void> {
  const ctx = await requireOperatorContext();
  // Chave já revogada (falso) não é erro: a tela só se atualiza.
  if ((await revokeReadKey(ctx, keyId)) === null) notFound();
  refresh();
}
