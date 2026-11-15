// Testes dos conferidores de dependencias. Rodam com: node --test scripts/deps/*.test.mjs
import assert from "node:assert/strict";
import { test } from "node:test";
import { check, evaluate, parseGo, parseNpm } from "./check-licenses.mjs";
import { interpret } from "./audit-pnpm.mjs";

const allowlist = { allowed: ["MIT", "Apache-2.0", "BSD-3-Clause"], exceptions: [] };
const dep = (license, name = "pkg") => ({ ecosystem: "npm", name, version: "1.0.0", license });
const fails = (license) => check([dep(license)], allowlist).violations.length === 1;

test("licenca permitida passa", () => {
  assert.equal(check([dep("MIT")], allowlist).violations.length, 0);
});

test("licenca fora da allowlist falha e aparece na lista", () => {
  const { violations } = check([dep("MIT"), dep("GPL-3.0-only", "ruim")], allowlist);
  assert.equal(violations.length, 1);
  assert.equal(violations[0].name, "ruim");
  assert.equal(violations[0].license, "GPL-3.0-only");
});

test("sem licenca, UNLICENSED, Unknown e SEE LICENSE falham", () => {
  for (const l of ["", "UNLICENSED", "Unknown", "SEE LICENSE IN LICENSE.md", "Custom: https://x"]) {
    assert.ok(fails(l), `deveria falhar: "${l}"`);
  }
});

test("OR: basta uma alternativa permitida (dual license)", () => {
  assert.ok(!fails("(MIT OR GPL-3.0-only)"));
  assert.ok(!fails("GPL-3.0-only OR Apache-2.0"));
  assert.ok(fails("(GPL-3.0-only OR AGPL-3.0-only)"));
});

test("AND: todas precisam ser permitidas", () => {
  assert.ok(!fails("MIT AND Apache-2.0"));
  assert.ok(fails("MIT AND CC-BY-4.0"));
  assert.ok(fails("MIT AND (Apache-2.0 AND GPL-2.0-only)"));
});

test("AND tem precedencia sobre OR e parenteses valem", () => {
  assert.equal(evaluate("MIT OR GPL AND LGPL", (id) => id === "MIT").ok, true);
  assert.equal(evaluate("(MIT OR GPL) AND LGPL", (id) => id === "MIT").ok, false);
});

test("WITH vale como a licenca base", () => {
  assert.ok(!fails("Apache-2.0 WITH LLVM-exception"));
  assert.ok(fails("GPL-2.0-only WITH Classpath-exception-2.0"));
});

test("expressao invalida falha em vez de passar", () => {
  for (const l of ["MIT OR", "(MIT", "MIT)", "AND MIT", "MIT WITH", "MIT AND AND Apache-2.0"]) {
    assert.ok(fails(l), `deveria falhar: "${l}"`);
  }
});

test("excecao libera so a licenca e o pacote exatos", () => {
  const al = {
    ...allowlist,
    exceptions: [{ ecosystem: "npm", package: "dados", license: "CC-BY-4.0", reason: "x", date: "2026-01-01", expires: "2026-07-01" }],
  };
  const now = new Date("2026-06-01T00:00:00Z");
  assert.equal(check([dep("CC-BY-4.0", "dados")], al, now).violations.length, 0);
  assert.equal(check([dep("CC-BY-4.0", "outro")], al, now).violations.length, 1);
  assert.equal(check([dep("GPL-3.0-only", "dados")], al, now).violations.length, 1, "mudou a licenca: excecao nao vale");
});

test("excecao vencida ou incompleta falha", () => {
  const ex = { ecosystem: "npm", package: "dados", license: "CC-BY-4.0", reason: "x", date: "2026-01-01", expires: "2026-03-01" };
  const now = new Date("2026-06-01T00:00:00Z");
  const venc = check([dep("CC-BY-4.0", "dados")], { ...allowlist, exceptions: [ex] }, now);
  assert.match(venc.violations[0].reason, /vencida/);
  const inc = check([dep("CC-BY-4.0", "dados")], { ...allowlist, exceptions: [{ ...ex, reason: "" }] }, new Date("2026-02-01T00:00:00Z"));
  assert.match(inc.violations[0].reason, /incompleta/);
});

test("excecao com data invalida ou prazo longo falha (nunca vale para sempre)", () => {
  const base = { ecosystem: "npm", package: "dados", license: "CC-BY-4.0", reason: "x", date: "2026-01-01", expires: "2026-03-01" };
  const now = new Date("2026-02-01T00:00:00Z");
  const run = (ex) => check([dep("CC-BY-4.0", "dados")], { ...allowlist, exceptions: [{ ...base, ...ex }] }, now);
  assert.equal(run({}).violations.length, 0, "controle: a excecao valida passa");
  for (const expires of ["soon", "2026-1-1x", "2026-02-30", "2026-03-01T00:00:00Z", undefined]) {
    assert.match(run({ expires }).violations[0]?.reason ?? "passou", /invalid/, `expires ${expires}`);
  }
  assert.match(run({ date: "ontem" }).violations[0]?.reason ?? "passou", /invalid/);
  assert.match(run({ expires: "2026-08-01" }).violations[0]?.reason ?? "passou", /prazo maior que 183/, "mais de seis meses");
  assert.equal(run({ expires: "2026-07-02" }).violations.length, 0, "183 dias passa");
  assert.match(run({ date: "2026-03-01", expires: "2026-01-01" }).violations[0]?.reason ?? "passou", /anterior/);
});

test("prefixo por barra so vale para Go; npm so com asterisco", () => {
  const ex = (ecosystem, pkg) => ({ ...allowlist, exceptions: [{ ecosystem, package: pkg, license: "MPL-2.0", reason: "x", date: "2026-01-01", expires: "2026-03-01" }] });
  const now = new Date("2026-02-01T00:00:00Z");
  const n = (name) => ({ ecosystem: "npm", name, version: "1", license: "MPL-2.0" });
  assert.equal(check([n("@escopo/x")], ex("npm", "@escopo"), now).violations.length, 1);
  assert.equal(check([n("@escopo/x")], ex("npm", "@escopo/*"), now).violations.length, 0);
  assert.equal(check([n("@escopo/y")], ex("npm", "@escopo/x"), now).violations.length, 1);
});

test("excecao de Go vale para subpacotes do modulo", () => {
  const al = { ...allowlist, exceptions: [{ ecosystem: "go", package: "example.com/m", license: "MPL-2.0", reason: "x", date: "2026-01-01", expires: "2026-07-01" }] };
  const d = { ecosystem: "go", name: "example.com/m/internal", version: "v1", license: "MPL-2.0" };
  assert.equal(check([d], al, new Date("2026-06-01T00:00:00Z")).violations.length, 0);
});

test("parseNpm e parseGo", () => {
  const npm = parseNpm(JSON.stringify({ MIT: [{ name: "a", versions: ["1.0.0"], license: "MIT" }] }));
  assert.deepEqual(npm, [{ ecosystem: "npm", name: "a", version: "1.0.0", license: "MIT" }]);
  const go = parseGo("go: downloading x\nexample.com/m\tv1.2.3\tApache-2.0\n\n");
  assert.deepEqual(go, [{ ecosystem: "go", name: "example.com/m", version: "v1.2.3", license: "Apache-2.0" }]);
});

// --- audit ----------------------------------------------------------------

const report = (severities) =>
  JSON.stringify({
    advisories: Object.fromEntries(
      severities.map((s, i) => [
        String(i),
        { severity: s, module_name: `m${i}`, vulnerable_versions: "<2", patched_versions: ">=2", github_advisory_id: "GHSA-x", title: "t", findings: [{ version: "1", paths: ["a>b"] }] },
      ]),
    ),
    metadata: { vulnerabilities: {}, totalDependencies: 10 },
  });

test("audit: sem vulnerabilidade e moderate passam", () => {
  assert.equal(interpret(report([])).code, 0);
  assert.equal(interpret(report(["moderate", "low"])).code, 0);
});

test("audit: high e critical falham", () => {
  assert.equal(interpret(report(["moderate", "high"])).code, 1);
  assert.equal(interpret(report(["critical"])).code, 1);
});

test("audit: falha de consulta nunca vira verde", () => {
  const rede = JSON.stringify({ error: { code: "ECONNREFUSED", message: "request failed" } });
  assert.equal(interpret(rede).code, 2);
  assert.equal(interpret("").code, 2);
  assert.equal(interpret("<html>502 Bad Gateway</html>").code, 2);
  assert.equal(interpret("{}").code, 2);
  assert.equal(interpret(JSON.stringify({ advisories: {} })).code, 2);
});

test("audit: severidade desconhecida ou ausente e exit 2, nao verde nem TypeError", () => {
  assert.equal(interpret(report(["unknown"])).code, 2);
  assert.equal(interpret(report(["High"])).code, 2);
  const semSeveridade = JSON.stringify({ advisories: { 0: { module_name: "m", findings: [] } }, metadata: { vulnerabilities: {} } });
  assert.equal(interpret(semSeveridade).code, 2);
});

test("audit: contagem high/critical no metadata sem advisories e exit 2", () => {
  const incons = JSON.stringify({ advisories: {}, metadata: { vulnerabilities: { high: 2, critical: 0 }, totalDependencies: 5 } });
  assert.equal(interpret(incons).code, 2);
  const incons2 = JSON.stringify({ advisories: {}, metadata: { vulnerabilities: { critical: 1 } } });
  assert.equal(interpret(incons2).code, 2);
});

test("audit: null, array e primitivos sao exit 2", () => {
  for (const t of ["null", "[]", "42", '"x"', "true"]) assert.equal(interpret(t).code, 2, t);
});
