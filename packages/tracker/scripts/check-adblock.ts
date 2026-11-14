// Confere se as listas de bloqueio mais usadas bloqueiam o script ou os endpoints do gateway (#23).
// Baixa as listas na hora (nada é commitado) e avalia cada requisição com o matcher de regras de
// rede do @ghostery/adblocker, como se ela partisse da página de um tenant.
//
// Uso: pnpm --filter @notify/tracker check:adblock [host...]
// Os hosts passados como argumento entram na lista dos que não podem ser bloqueados.
import { FiltersEngine, Request } from "@ghostery/adblocker";
import type { RequestType } from "@ghostery/adblocker";
import { readManifest } from "./bundle.ts";

const UBO = "https://ublockorigin.github.io/uAssets/filters";

// Listas ligadas por padrão no uBlock Origin (exceto a de URLs maliciosas, que só tem domínios
// de malware), que incluem a EasyList e a EasyPrivacy.
const lists: Record<string, string> = {
  EasyList: "https://easylist.to/easylist/easylist.txt",
  EasyPrivacy: "https://easylist.to/easylist/easyprivacy.txt",
  "uBO filters": `${UBO}/filters.txt`,
  "uBO privacy": `${UBO}/privacy.txt`,
  "uBO badware": `${UBO}/badware.txt`,
  "uBO quick-fixes": `${UBO}/quick-fixes.txt`,
  "uBO unbreak": `${UBO}/unbreak.txt`,
  "Peter Lowe": "https://pgl.yoyo.org/adservers/serverlist.php?hostformat=adblockplus&showintro=0&mimetype=plaintext",
};

// Hosts que não podem ser bloqueados: o exemplo neutro da documentação e os passados na linha de
// comando (aceita "e.foo.org" ou "https://e.foo.org/").
const requiredHosts = [
  "gw.example.com",
  ...process.argv.slice(2).map((arg) => (arg.includes("://") ? new URL(arg).host : arg.replace(/\/.*$/, ""))),
];

// Subdomínios que alguém poderia escolher em produção. Só informativo: mostra quais nomes evitar.
const candidateHosts = [
  "notify",
  "ingest",
  "events",
  "stats",
  "metrics",
  "telemetry",
  "insights",
  "analytics",
  "track",
  "tracking",
  "collect",
  "umami",
].map((label) => `${label}.example.com`);

// Página de terceiro (o caso comum) e página no mesmo domínio do gateway.
const sources = ["https://www.tenant-site.example.net/", "https://www.example.com/"];

const websiteId = "5f2b6c1e-8a43-4d1f-9b7e-3c0a2d4e6f81";

interface Probe {
  label: string;
  path: string;
  type: RequestType;
}

const probes: Probe[] = [
  ...Object.keys(readManifest()).map((version) => ({
    label: `GET /s/v${version}/n.js`,
    path: `/s/v${version}/n.js`,
    type: "script" as const,
  })),
  { label: "POST /e", path: "/e", type: "xmlhttprequest" },
  { label: "GET /c", path: `/c?s=${websiteId}`, type: "xmlhttprequest" },
];

// Controles que as listas precisam bloquear: se algum passar, o matcher ou as listas quebraram e o
// resultado acima não vale. Os dois primeiros são os caminhos padrão do Umami no host do gateway.
const controls: { url: string; type: RequestType }[] = [
  { url: `https://${requiredHosts[0]}/umami.js`, type: "script" },
  { url: `https://${requiredHosts[0]}/api/collect`, type: "xmlhttprequest" },
  { url: "https://www.google-analytics.com/analytics.js", type: "script" },
];

async function download(url: string): Promise<string> {
  for (let attempt = 1; ; attempt++) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(30_000) });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return await response.text();
    } catch (error) {
      if (attempt === 3) throw new Error(`falha ao baixar ${url}: ${error}`);
      await new Promise((resolve) => setTimeout(resolve, 2_000 * attempt));
    }
  }
}

// Resolve os "!#include" (o filters.txt do uBO divide as regras em vários arquivos).
async function downloadList(url: string): Promise<string> {
  const text = await download(url);
  const parts = await Promise.all(
    text.split("\n").map(async (line) => {
      const include = /^!#include\s+(\S+)/.exec(line.trim());
      return include ? downloadList(new URL(include[1]!, url).href) : line;
    }),
  );
  return parts.join("\n");
}

// Só regras de rede; os filtros cosméticos escondem elementos e não afetam requisições.
function engineFor(text: string): FiltersEngine {
  return FiltersEngine.parse(text, { loadCosmeticFilters: false, loadNetworkFilters: true });
}

const texts = Object.fromEntries(
  await Promise.all(
    Object.entries(lists).map(async ([name, url]) => [name, await downloadList(url)] as const),
  ),
);

// O veredito vem das listas juntas, como no navegador (uma exceção de uma lista vale para as outras).
const combined = engineFor(Object.values(texts).join("\n"));
const perList = Object.entries(texts).map(([name, text]) => [name, engineFor(text)] as const);

// Cada lista precisa virar regras de rede de verdade: uma página de erro, captcha ou limite de taxa
// devolvida com 200 tem linhas de texto, mas nenhuma regra, e faria o teste passar sem testar nada.
const MIN_NETWORK_FILTERS = 20;
console.log(`Listas baixadas em ${new Date().toISOString()}:`);
for (const [name, engine] of perList) {
  const text = texts[name]!;
  const filters = engine.getFilters().networkFilters.length;
  const version = /^! (?:Version|Last modified):\s*(.+)$/m.exec(text)?.[1]?.trim() ?? "sem versão";
  console.log(`  ${name}: ${filters} regras de rede (${version})`);
  if (filters < MIN_NETWORK_FILTERS) throw new Error(`a lista ${name} virou só ${filters} regras de rede`);
}

// Devolve as regras que bloqueiam a requisição, com a lista de origem, ou [] se ela passa.
function blockedBy(host: string, probe: Probe, sourceUrl: string): string[] {
  const request = Request.fromRawDetails({
    url: `https://${host}${probe.path}`,
    sourceUrl,
    type: probe.type,
  });
  const { match, redirect } = combined.match(request);
  if (!match && !redirect) return [];
  const reasons = perList.flatMap(([name, engine]) => {
    const { filter } = engine.match(request);
    return filter ? [`${name}: ${filter.toString()}`] : [];
  });
  return reasons.length > 0 ? reasons : ["(combinação de listas)"];
}

function check(host: string): string[] {
  return probes.flatMap((probe) =>
    sources.flatMap((source) =>
      blockedBy(host, probe, source).map(
        (reason) => `${probe.label} em ${host} a partir de ${new URL(source).hostname}: ${reason}`,
      ),
    ),
  );
}

let failures = 0;
console.log("\nControles (precisam ser bloqueados):");
for (const control of controls) {
  const request = Request.fromRawDetails({ ...control, sourceUrl: sources[0] });
  const { match, filter } = combined.match(request);
  if (!match) failures++;
  console.log(`  ${match ? "bloqueado" : "PASSOU   "} ${control.url}${filter ? ` (${filter.toString()})` : ""}`);
}

console.log("\nHosts que não podem ser bloqueados:");
for (const host of requiredHosts) {
  const blocks = check(host);
  failures += blocks.length;
  console.log(`  ${blocks.length === 0 ? "ok       " : "BLOQUEADO"} ${host}`);
  for (const block of blocks) console.log(`    ${block}`);
}

// sendBeacon vira uma requisição do tipo ping, que a EasyPrivacy bloqueia de terceiros ($ping,3p).
// Informativo: mostra por que o script usa fetch com keepalive e nunca sendBeacon.
const beacon = blockedBy(requiredHosts[0]!, { label: "POST /e (sendBeacon)", path: "/e", type: "ping" }, sources[0]!);
console.log("\nPOST /e como sendBeacon (informativo; o script usa fetch):");
console.log(`  ${beacon.length === 0 ? "ok       " : "bloqueado"} ${beacon[0] ?? ""}`);

console.log("\nSubdomínios candidatos (informativo):");
for (const host of candidateHosts) {
  const blocks = check(host);
  console.log(`  ${blocks.length === 0 ? "ok       " : "bloqueado"} ${host}`);
  for (const block of [...new Set(blocks.map((b) => b.replace(/^.*?: /, "")))]) {
    console.log(`    ${block}`);
  }
}

if (failures > 0) {
  console.error(`\n${failures} falha(s): requisição bloqueada num host obrigatório ou controle que passou.`);
  process.exit(1);
}
console.log("\nNenhuma requisição bloqueada nos hosts obrigatórios.");
