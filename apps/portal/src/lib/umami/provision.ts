import "server-only";
import { umamiRequest } from "./http";

// Cria o site no Umami e depois grava o vínculo no `stellytics` (spec do E2,
// seção 7). Se a gravação falhar, apaga o site recém-criado no Umami. O
// portal cria só sites, nunca usuários nem equipes (ADR 0003).
//
// Recebe a gravação como função para que o ID do Umami nunca saia daqui sem
// estar ligado a um site do tenant. Chamado só por src/lib/data/sites.ts.
export async function provisionUmamiWebsite<T>(
  input: { name: string; domain: string },
  persist: (umamiWebsiteId: string) => Promise<T>,
): Promise<T> {
  const website = await umamiRequest<{ id: string }>("/api/websites", {
    method: "POST",
    body: { name: input.name, domain: input.domain },
  });
  try {
    return await persist(website.id);
  } catch (error) {
    await umamiRequest(`/api/websites/${website.id}`, { method: "DELETE" }).catch((cleanup) => {
      // Sobra um site órfão no Umami; o teste de consistência o encontra.
      console.error("Falha ao desfazer o site no Umami", website.id, cleanup);
    });
    throw error;
  }
}
