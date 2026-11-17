import { getSessionCookie } from "better-auth/cookies";
import { type NextRequest, NextResponse } from "next/server";

// Redirecionamento otimista: só confere se o cookie de sessão existe.
// A validação de verdade acontece no servidor (lib/session.ts).
export function proxy(request: NextRequest) {
  // O otimizador está desligado e sua biblioteca não acompanha a imagem.
  // O matcher normaliza a barra final e testa também o caminho decodificado,
  // então a comparação repete isso: sem isso, /_next/image/ ou /_next/%69mage
  // passariam pelo matcher e cairiam no redirecionamento de sessão.
  const pathname = request.nextUrl.pathname.replace(/\/+$/, "");
  let decoded = pathname;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    // Percent-encoding inválido: segue sem decodificar; o Next responde depois.
  }
  if (decoded === "/_next/image") {
    return new NextResponse(null, { status: 404 });
  }

  if (!getSessionCookie(request)) {
    const url = new URL("/sign-in", request.url);
    url.searchParams.set("next", request.nextUrl.pathname);
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = {
  matcher: [
    "/_next/image",
    "/dashboard/:path*",
    "/operator/:path*",
    "/services/:path*",
    "/sites/:path*",
    "/onboarding/:path*",
  ],
};
