import { getSessionCookie } from "better-auth/cookies";
import { type NextRequest, NextResponse } from "next/server";

// Redirecionamento otimista: só confere se o cookie de sessão existe.
// A validação de verdade acontece no servidor (lib/session.ts).
export function proxy(request: NextRequest) {
  // O otimizador está desligado e sua biblioteca não acompanha a imagem.
  if (request.nextUrl.pathname === "/_next/image") {
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
