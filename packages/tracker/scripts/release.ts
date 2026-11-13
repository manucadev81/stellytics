// Publica a versão do package.json em published/vX.Y.Z/n.js e registra o SRI em published/sri.json.
// Versão publicada é imutável: o script se recusa a sobrescrever uma versão que já existe.
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { bundle, currentVersion, fileName, publishedDir, readManifest, sri, sriFile } from "./bundle.ts";

const version = currentVersion();
const dir = join(publishedDir, `v${version}`);
const manifest = existsSync(sriFile) ? readManifest() : {};

if (existsSync(dir) || manifest[version]) {
  console.error(`A versão ${version} já foi publicada e não pode mudar. Suba a versão no package.json.`);
  process.exit(1);
}

const content = await bundle();
mkdirSync(dir, { recursive: true });
writeFileSync(join(dir, fileName), content);
manifest[version] = sri(content);
writeFileSync(sriFile, JSON.stringify(manifest, null, 2) + "\n");
console.log(`published/v${version}/${fileName} ${manifest[version]}`);
