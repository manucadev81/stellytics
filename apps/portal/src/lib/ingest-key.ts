import { createHash, randomBytes } from "node:crypto";

// Formato da chave de ingestão (spec do E2, seção 5). É o contrato com o
// coletor (#30), que valida a chave pelo mesmo hash.
//
//   ntf_<prefixo>_<segredo>
//   prefixo: 8 caracteres base32 minúsculos (40 bits), público
//   segredo: 32 bytes do gerador criptográfico, em base64url (43 caracteres)
//
// O banco guarda só o prefixo e o SHA-256 da chave completa. A chave completa
// existe só na resposta da criação: nunca vai para log, banco ou erro.

const BASE32 = "abcdefghijklmnopqrstuvwxyz234567";

export const KEY_PATTERN = /^ntf_([a-z2-7]{8})_([A-Za-z0-9_-]{43})$/;

// 5 bytes = 40 bits = exatamente 8 dígitos base32.
function randomPrefix(): string {
  const bytes = randomBytes(5);
  let bits = 0n;
  for (const byte of bytes) bits = (bits << 8n) | BigInt(byte);
  let out = "";
  for (let i = 7; i >= 0; i--) out += BASE32[Number((bits >> BigInt(i * 5)) & 31n)];
  return out;
}

export function hashIngestKey(key: string): Buffer {
  return createHash("sha256").update(key, "utf8").digest();
}

export function generateIngestKey(): { key: string; prefix: string; keyHash: Buffer } {
  const prefix = randomPrefix();
  const key = `ntf_${prefix}_${randomBytes(32).toString("base64url")}`;
  return { key, prefix, keyHash: hashIngestKey(key) };
}
