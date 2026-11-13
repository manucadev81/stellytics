// Garante que versão publicada não muda depois de publicada (#22).
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { bundle, currentVersion, fileName, publishedDir, readManifest, sri } from "../scripts/bundle.ts";

const manifest = readManifest();
const versions = readdirSync(publishedDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name.replace(/^v/, ""));

describe("versões publicadas", () => {
  it("cada pasta tem um SRI registrado e vice-versa", () => {
    expect(versions.sort()).toEqual(Object.keys(manifest).sort());
  });

  it.each(versions)("v%s confere com o SRI registrado", (version) => {
    const content = readFileSync(join(publishedDir, `v${version}`, fileName));
    expect(sri(content)).toBe(manifest[version]);
  });

  it("o código atual é o da versão do package.json", async () => {
    const version = currentVersion();
    const file = join(publishedDir, `v${version}`, fileName);
    expect(existsSync(file), `rode "pnpm --filter @notify/tracker release" para publicar a v${version}`).toBe(
      true,
    );
    // Mudou o código sem subir a versão? Suba a versão no package.json e rode o release.
    expect(sri(await bundle())).toBe(manifest[version]);
  });
});
