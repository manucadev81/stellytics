"use server";

import { refresh } from "next/cache";
import { requireTenantContext } from "@/lib/access";
import {
  ApiServiceError,
  createApiService,
  createIngestKey,
  revokeIngestKey,
} from "@/lib/data/api-services";

// Server actions dos serviços de API e chaves (#59). São endpoints públicos:
// cada uma resolve o tenant pela sessão e passa IDs vindos do cliente para a
// camada de dados, que confere se são do tenant.
//
// A chave completa volta só no estado da ação, uma única vez, e nunca é
// registrada em log.

export type KeyActionState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; key: string; prefix: string; serviceId: string };

function errorState(error: unknown): KeyActionState {
  if (error instanceof ApiServiceError) return { status: "error", message: error.message };
  // Erro inesperado: a mensagem não chega ao cliente e não leva a chave.
  console.error("Falha em ação de chave de ingestão", (error as Error).name);
  return { status: "error", message: "Something went wrong. Try again." };
}

export async function createServiceAction(_prev: KeyActionState, form: FormData): Promise<KeyActionState> {
  const ctx = await requireTenantContext();
  try {
    const { service, firstKey } = await createApiService(ctx, { name: String(form.get("name") ?? "") });
    refresh();
    return { status: "created", key: firstKey.key, prefix: firstKey.summary.prefix, serviceId: service.id };
  } catch (error) {
    return errorState(error);
  }
}

export async function createKeyAction(serviceId: string): Promise<KeyActionState> {
  const ctx = await requireTenantContext();
  try {
    const created = await createIngestKey(ctx, serviceId);
    if (!created) return { status: "error", message: "Service not found." };
    refresh();
    return { status: "created", key: created.key, prefix: created.summary.prefix, serviceId };
  } catch (error) {
    return errorState(error);
  }
}

export async function revokeKeyAction(keyId: string): Promise<void> {
  const ctx = await requireTenantContext();
  await revokeIngestKey(ctx, keyId);
  refresh();
}
