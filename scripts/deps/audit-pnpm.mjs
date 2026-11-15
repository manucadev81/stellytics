// Roda "pnpm audit --prod --json" e falha em vulnerabilidade high ou critical.
//
// Distingue tres resultados, porque o pnpm sai com 1 tanto para "achei
// vulnerabilidade" quanto para "nao consegui consultar" (rede, registry fora,
// endpoint fora do ar), e este ultimo nao pode virar verde:
//   0  consulta feita, nada high/critical (moderate e low so sao listadas)
//   1  consulta feita, ha vulnerabilidade high ou critical
//   2  nao consegui consultar ou a resposta nao e um relatorio de audit
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

const SEVERITIES = ["info", "low", "moderate", "high", "critical"];
const FAIL_FROM = "high";

// Aceita o texto de "pnpm audit --json" (ou use --file <arquivo> para teste).
export function interpret(stdout) {
  let report;
  try {
    report = JSON.parse(stdout);
  } catch {
    return { code: 2, message: "A saida do pnpm audit nao e JSON." };
  }
  if (report.error) {
    return { code: 2, message: `Nao consegui consultar o audit: ${report.error.code ?? ""} ${report.error.message ?? ""}`.trim() };
  }
  if (typeof report.advisories !== "object" || report.advisories === null || !report.metadata?.vulnerabilities) {
    return { code: 2, message: "A resposta nao tem advisories nem metadata: nao e um relatorio de audit." };
  }
  const list = Object.values(report.advisories);
  const lines = list.map(
    (a) =>
      `  ${a.severity.padEnd(8)} ${a.module_name}  vulneravel: ${a.vulnerable_versions}  corrigido em: ${a.patched_versions}  ${a.github_advisory_id ?? ""}  ${a.title}\n` +
      a.findings.flatMap((f) => f.paths.map((p) => `             ${f.version} via ${p}`)).join("\n"),
  );
  const blocking = list.filter((a) => SEVERITIES.indexOf(a.severity) >= SEVERITIES.indexOf(FAIL_FROM));
  const summary = `Consulta feita: ${list.length} advisory(ies); ${blocking.length} high/critical. Pacotes: ${report.metadata.totalDependencies}.`;
  const body = lines.length ? `\n${lines.join("\n")}\n` : "";
  return { code: blocking.length > 0 ? 1 : 0, message: summary + body };
}

function main() {
  const i = process.argv.indexOf("--file");
  let stdout;
  if (i !== -1) {
    stdout = readFileSync(process.argv[i + 1], "utf8");
  } else {
    const r = spawnSync("pnpm", ["audit", "--prod", "--json"], { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
    if (r.error) {
      console.error(`Nao consegui rodar o pnpm: ${r.error.message}`);
      process.exit(2);
    }
    stdout = r.stdout;
  }
  const { code, message } = interpret(stdout);
  (code === 2 ? console.error : console.log)(message);
  process.exit(code);
}

import { fileURLToPath } from "node:url";
if (process.argv[1] === fileURLToPath(import.meta.url)) main();
