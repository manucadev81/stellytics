// Prepara a conta de serviço do portal no Umami e imprime uma chave de API nova.
// Roda uma vez por ambiente (e de novo para trocar a chave), por quem administra
// o servidor. O portal guarda só a chave, em UMAMI_API_KEY; nunca a senha de
// administrador (ADR 0001, verificado na #58).
//
// Uso:
//   read -rs UMAMI_ADMIN_PASSWORD && export UMAMI_ADMIN_PASSWORD
//   UMAMI_URL=http://127.0.0.1:3001 pnpm umami:service-account [--revoke-old]
//
// O que faz:
//   1. Entra no Umami como administrador (UMAMI_ADMIN_USER, padrão "admin").
//   2. Cria o usuário de serviço (UMAMI_SERVICE_USER, padrão "notify-portal")
//      com o papel comum "user", não administrador. Se ele já existe, troca a
//      senha por uma nova aleatória.
//   3. Entra como o usuário de serviço e cria uma chave de API.
//   4. Com --revoke-old, apaga as outras chaves do usuário de serviço. Use para
//      trocar a chave: rode sem a opção, ponha a chave nova no portal e rode de
//      novo com ela (o que cria mais uma chave e deixa só essa última).
// A senha do usuário de serviço é aleatória e descartada: ninguém entra com ela.
// Só a chave sai na saída padrão; as mensagens vão para a saída de erro.
import { randomBytes } from "node:crypto";
import { parseArgs } from "node:util";

const { values: options } = parseArgs({ options: { "revoke-old": { type: "boolean", default: false } } });

const baseUrl = (process.env.UMAMI_URL ?? "http://127.0.0.1:3001").replace(/\/$/, "");
const adminUser = process.env.UMAMI_ADMIN_USER ?? "admin";
const adminPassword = process.env.UMAMI_ADMIN_PASSWORD;
const serviceUser = process.env.UMAMI_SERVICE_USER ?? "notify-portal";

if (!adminPassword) {
  console.error("Informe a senha de administrador do Umami em UMAMI_ADMIN_PASSWORD.");
  process.exit(1);
}

async function call(path, { token, method = "GET", body } = {}) {
  const response = await fetch(`${baseUrl}${path}`, {
    method,
    headers: {
      accept: "application/json",
      ...(body ? { "content-type": "application/json" } : {}),
      ...(token ? { authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) {
    throw new Error(`${method} ${path} respondeu ${response.status}: ${await response.text()}`);
  }
  return response.json();
}

async function login(username, password) {
  const { token } = await call("/api/auth/login", { method: "POST", body: { username, password } });
  return token;
}

const adminToken = await login(adminUser, adminPassword);
const servicePassword = randomBytes(32).toString("base64url");

const { data: found } = await call(
  `/api/admin/users?search=${encodeURIComponent(serviceUser)}&pageSize=100`,
  { token: adminToken },
);
const existing = found.find((u) => u.username === serviceUser);

if (existing) {
  if (existing.role !== "user") {
    throw new Error(`O usuário ${serviceUser} existe com o papel ${existing.role}; esperado "user".`);
  }
  await call(`/api/users/${existing.id}`, {
    token: adminToken,
    method: "POST",
    body: { password: servicePassword },
  });
  console.error(`Usuário de serviço ${serviceUser} já existia; senha trocada.`);
} else {
  await call("/api/users", {
    token: adminToken,
    method: "POST",
    body: { username: serviceUser, password: servicePassword, role: "user" },
  });
  console.error(`Usuário de serviço ${serviceUser} criado com o papel "user".`);
}

const serviceToken = await login(serviceUser, servicePassword);
const name = `notify-portal ${new Date().toISOString()}`;
const { id: newKeyId, key, keyPrefix } = await call("/api/me/api-keys", {
  token: serviceToken,
  method: "POST",
  body: { name },
});
if (options["revoke-old"]) {
  const keys = await call("/api/me/api-keys", { token: serviceToken });
  for (const old of keys.filter((k) => k.id !== newKeyId)) {
    await call(`/api/me/api-keys/${old.id}`, { token: serviceToken, method: "DELETE" });
    console.error(`Chave ${old.keyPrefix}… revogada.`);
  }
}
console.error(`Chave ${keyPrefix}… criada. Guarde-a em UMAMI_API_KEY; ela não aparece de novo.`);
console.log(key);
