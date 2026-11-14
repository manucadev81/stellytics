import { defineConfig, devices } from "@playwright/test";

// Rode pelo run.mjs (pnpm --filter @notify/examples-e2e e2e), que cria os
// sites e preenche SITE_STATIC, SITE_SPA e SITE_NEXT.
const gateway = process.env.NOTIFY_GATEWAY_URL ?? "http://127.0.0.1:3002";

function server(name: string, port: number, command: string, cwd: string, site?: string) {
  return {
    name,
    command,
    cwd,
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    env: { PORT: String(port), NOTIFY_WEBSITE_ID: site ?? "", NOTIFY_GATEWAY_URL: gateway, NEXT_TELEMETRY_DISABLED: "1" },
  };
}

export default defineConfig({
  testDir: "tests",
  timeout: 60_000,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["github"]] : "list",
  use: { ...devices["Desktop Chrome"], trace: "retain-on-failure" },
  webServer: [
    server("static-html", 4001, "node ../serve.mjs .", "../static-html", process.env.SITE_STATIC),
    server("spa", 4002, "node ../serve.mjs . spa", "../spa", process.env.SITE_SPA),
    server(
      "nextjs-ssr",
      4003,
      "node_modules/.bin/next build && node_modules/.bin/next start --hostname 127.0.0.1 --port 4003",
      "../nextjs-ssr",
      process.env.SITE_NEXT,
    ),
  ],
});
