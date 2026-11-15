// Roda "pnpm audit --prod --json" e falha em vulnerabilidade high ou critical.
// Uma segunda passada sem --prod so lista as de devDependencies.
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
  if (typeof report !== "object" || report === null || Array.isArray(report)) {
    return { code: 2, message: "A saida do pnpm audit nao e um objeto JSON." };
  }
  if (report.error) {
    return { code: 2, message: `Nao consegui consultar o audit: ${report.error.code ?? ""} ${report.error.message ?? ""}`.trim() };
  }
  if (typeof report.advisories !== "object" || report.advisories === null || !report.metadata?.vulnerabilities) {
    return { code: 2, message: "A resposta nao tem advisories nem metadata: nao e um relatorio de audit." };
  }
  const list = Object.values(report.advisories);
  // Severidade desconhecida ou ausente ("unknown", "High") nao pode virar verde.
  const bad = list.filter((a) => !SEVERITIES.includes(a?.severity));
  if (bad.length > 0) {
    return { code: 2, message: `Severidade desconhecida no relatorio do audit: ${bad.map((a) => JSON.stringify(a?.severity)).join(", ")}. Esperado: ${SEVERITIES.join(", ")}.` };
  }
  // Os totais do metadata precisam bater com a lista; senao a resposta esta incompleta.
  for (const level of ["high", "critical"]) {
    const total = Number(report.metadata.vulnerabilities[level] ?? 0);
    const listed = list.filter((a) => a.severity === level).length;
    if (!Number.isFinite(total) || (total > 0 && listed === 0)) {
      return { code: 2, message: `O metadata diz ${total} ${level} mas a lista de advisories tem ${listed}: resposta incompleta ou inconsistente.` };
    }
  }
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

function run(args) {
  const r = spawnSync("pnpm", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  if (r.error) return { error: `Nao consegui rodar o pnpm: ${r.error.message}` };
  return { stdout: r.stdout };
}

// Segunda passada, sem --prod, so para LISTAR o que ha nas devDependencies
// (ex.: o esbuild que gera o n.js servido a todos os tenants). Nunca altera o
// resultado: o veredito vem so da passada de producao.
function devOnlyNote(prodStdout) {
  const all = run(["audit", "--json"]);
  if (all.error) return `aviso: passada completa nao rodou (${all.error})`;
  let a, p;
  try {
    a = JSON.parse(all.stdout);
    p = JSON.parse(prodStdout);
  } catch {
    return "aviso: passada completa (sem --prod) ilegivel; so a de producao vale";
  }
  if (!a?.advisories || !p?.advisories) return "aviso: passada completa (sem --prod) sem relatorio; so a de producao vale";
  const extra = Object.entries(a.advisories).filter(([id]) => !(id in p.advisories));
  if (extra.length === 0) return "Passada completa (sem --prod): nenhum advisory a mais, so de desenvolvimento.";
  return (
    "Somente em devDependencies (informativo, nao falha o job):\n" +
    extra.map(([, v]) => `  ${v.severity} ${v.module_name} ${v.github_advisory_id ?? ""} ${v.title}\n` + v.findings.flatMap((f) => f.paths.map((x) => `             ${f.version} via ${x}`)).join("\n")).join("\n")
  );
}

function main() {
  const i = process.argv.indexOf("--file");
  let stdout;
  if (i !== -1) {
    stdout = readFileSync(process.argv[i + 1], "utf8");
  } else {
    const r = run(["audit", "--prod", "--json"]);
    if (r.error) {
      console.error(r.error);
      process.exit(2);
    }
    stdout = r.stdout;
  }
  const { code, message } = interpret(stdout);
  (code === 2 ? console.error : console.log)(message);
  if (code !== 2 && i === -1) console.log(`\n${devOnlyNote(stdout)}`);
  process.exit(code);
}

import { fileURLToPath } from "node:url";
if (process.argv[1] === fileURLToPath(import.meta.url)) main();
