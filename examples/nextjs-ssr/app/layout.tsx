import { readFileSync } from "node:fs";
import { join } from "node:path";
import Link from "next/link";
import type { ReactNode } from "react";

// A tag do script é montada no servidor a cada request (SSR), com o site e o
// gateway vindos do ambiente. Num site real, ela é só o trecho de HTML fixo
// do README, com o ID do site colado.
export const dynamic = "force-dynamic";

function tracker() {
  const website = process.env.STELLYTICS_WEBSITE_ID;
  if (!website) throw new Error("defina STELLYTICS_WEBSITE_ID com o ID do site no Umami");
  const gateway = (process.env.STELLYTICS_GATEWAY_URL ?? "http://127.0.0.1:3002").replace(/\/$/, "");
  const sri: Record<string, string> = JSON.parse(
    readFileSync(join(process.cwd(), "../../packages/tracker/published/sri.json"), "utf8"),
  );
  const version =
    process.env.STELLYTICS_SCRIPT_VERSION ??
    Object.keys(sri).sort((a, b) => a.localeCompare(b, undefined, { numeric: true })).at(-1)!;
  return { src: `${gateway}/s/v${version}/n.js`, integrity: sri[version], website };
}

export default function RootLayout({ children }: { children: ReactNode }) {
  const t = tracker();
  return (
    <html lang="pt-BR">
      <head>
        <script defer src={t.src} integrity={t.integrity} crossOrigin="anonymous" data-site={t.website} />
      </head>
      <body>
        <nav>
          <Link href="/">Início</Link> <Link href="/docs">Documentação</Link>
        </nav>
        {children}
      </body>
    </html>
  );
}
