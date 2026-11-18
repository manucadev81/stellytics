import "server-only";
import { umamiRequest, UmamiError } from "./http";

// Apaga um site no Umami (direito ao apagamento, #154). Só este módulo fala
// DELETE com o Umami; quem orquestra o apagamento é src/lib/data/erasure.ts.
//
// 404 vale como sucesso: repetir um apagamento que falhou depois do site já
// ter saído do Umami não pode travar de novo.
export async function deleteUmamiWebsite(umamiWebsiteId: string): Promise<void> {
  try {
    await umamiRequest(`/api/websites/${umamiWebsiteId}`, { method: "DELETE" });
  } catch (error) {
    if (error instanceof UmamiError && error.status === 404) return;
    throw error;
  }
}
