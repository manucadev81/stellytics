import "server-only";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { auth } from "./auth";

// Verificação real da sessão, no servidor. O proxy.ts só faz o redirecionamento rápido.
// Para ler dados, use os contextos de lib/access.ts, que partem desta função.
export async function requireSession() {
  const session = await auth.api.getSession({ headers: await headers() });
  if (!session) redirect("/sign-in");
  return session;
}
