import "server-only";
import { env } from "../env";

// Único ponto do portal que fala HTTP com o Umami (#58). Só os módulos de
// src/lib/umami/ importam este arquivo; o ESLint proíbe o resto do código.
// A credencial é a chave de API da conta de serviço, vinda de UMAMI_API_KEY.

export class UmamiError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "UmamiError";
  }
}

const TIMEOUT_MS = 10_000;

export async function umamiRequest<T>(
  path: string,
  { method = "GET", body }: { method?: "GET" | "POST" | "DELETE"; body?: unknown } = {},
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${env.umamiUrl}${path}`, {
      method,
      headers: {
        accept: "application/json",
        authorization: `Bearer ${env.umamiApiKey}`,
        ...(body === undefined ? {} : { "content-type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
  } catch (error) {
    throw new UmamiError(`Umami ${method} ${path}: sem resposta (${(error as Error).name})`);
  }
  // A mensagem não leva o corpo da resposta nem a chave.
  if (!response.ok) {
    throw new UmamiError(`Umami ${method} ${path}: ${response.status}`, response.status);
  }
  return (await response.json()) as T;
}
