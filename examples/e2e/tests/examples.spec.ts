// Os três exemplos registram page views no Umami, passando pelo gateway com a
// prova de execução de JS (#27). A conferência é feita no banco do Umami.
import { expect, test, type Page } from "@playwright/test";
import postgres from "postgres";

const umami = postgres(process.env.UMAMI_DATABASE_URL ?? "postgresql://umami:umami@127.0.0.1:5432/umami");
test.afterAll(() => umami.end());

function site(name: "SITE_STATIC" | "SITE_SPA" | "SITE_NEXT"): string {
  const id = process.env[name];
  if (!id) throw new Error(`${name} não definido: rode pelo run.mjs`);
  return id;
}

// Caminhos dos page views do site, em ordem alfabética.
async function pageviews(website: string): Promise<string[]> {
  const rows = await umami<{ url_path: string }[]>`
    SELECT url_path FROM website_event WHERE website_id = ${website} AND event_type = 1`;
  return rows.map((r) => r.url_path).sort();
}

async function expectPageviews(website: string, want: string[]) {
  await expect.poll(() => pageviews(website), { timeout: 30_000 }).toEqual([...want].sort());
}

// Marca a janela; se a página recarregar, a marca some.
async function mark(page: Page) {
  await page.evaluate(() => Object.assign(window, { __noReload: true }));
}
async function expectNoReload(page: Page) {
  expect(await page.evaluate(() => (window as { __noReload?: boolean }).__noReload)).toBe(true);
}

test("HTML estático: um page view por página carregada", async ({ page }) => {
  const website = site("SITE_STATIC");
  await page.goto("http://127.0.0.1:4001/");
  await expectPageviews(website, ["/"]);
  await page.getByRole("link", { name: "Documentação" }).click();
  await expect(page).toHaveURL(/\/docs\.html$/);
  await expectPageviews(website, ["/", "/docs.html"]);
});

test("SPA: conta cada mudança de rota sem recarregar", async ({ page }) => {
  const website = site("SITE_SPA");
  await page.goto("http://127.0.0.1:4002/");
  await expectPageviews(website, ["/"]);
  await mark(page);

  // O script junta navegações a menos de 300 ms uma da outra num page view só,
  // o da rota final. Como uma pessoa, o teste espera cada rota ser contada.
  await page.getByRole("link", { name: "Preços" }).click();
  await expect(page).toHaveTitle("SPA: preços");
  await expectPageviews(website, ["/", "/precos"]);
  await page.getByRole("link", { name: "Contato" }).click();
  await expect(page).toHaveTitle("SPA: contato");
  await expectPageviews(website, ["/", "/precos", "/contato"]);
  await page.goBack();
  await expect(page).toHaveURL(/\/precos$/);

  await expectNoReload(page);
  await expectPageviews(website, ["/", "/precos", "/contato", "/precos"]);
});

test("Next.js com SSR: a tag vem no HTML do servidor e a navegação do roteador conta", async ({ page, request }) => {
  const website = site("SITE_NEXT");
  const html = await (await request.get("http://127.0.0.1:4003/")).text();
  expect(html).toMatch(/<script[^>]+src="[^"]+\/s\/v\d+\.\d+\.\d+\/n\.js"/);
  expect(html).toContain('integrity="sha384-');
  expect(html).toContain(`data-site="${website}"`);

  await page.goto("http://127.0.0.1:4003/");
  await expectPageviews(website, ["/"]);
  await mark(page);
  await page.getByRole("link", { name: "Documentação" }).click();
  await expect(page).toHaveURL(/\/docs$/);
  await expectNoReload(page);
  await expectPageviews(website, ["/", "/docs"]);
});
