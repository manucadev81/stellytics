// Seed de desenvolvimento (#19): 2 projetos de teste e 1 operador SCF.
// Pode rodar mais de uma vez: o que já existe fica como está.
//
// Uso:
//   read -rs SEED_PASSWORD && export SEED_PASSWORD
//   pnpm db:seed
//
// Contas criadas (todas com a senha de SEED_PASSWORD e e-mail confirmado):
//   owner-a@seed.notify.localhost  dono do "Seed Project A"
//   owner-b@seed.notify.localhost  dono do "Seed Project B"
//   operator@seed.notify.localhost operador SCF (não é membro de tenant, D2)
// Cada projeto ganha um serviço de API com uma chave de ingestão (a chave não
// é mostrada; crie outra pela tela se precisar dela) e, se UMAMI_API_KEY
// estiver definida, um site no Umami.
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { hashPassword } from "better-auth/crypto";
import postgres from "postgres";

if (process.env.NODE_ENV === "production") {
  console.error("O seed é só para desenvolvimento.");
  process.exit(1);
}
const password = process.env.SEED_PASSWORD;
if (!password || password.length < 12) {
  console.error("Informe uma senha de pelo menos 12 caracteres em SEED_PASSWORD.");
  process.exit(1);
}

const DOMAIN = "seed.notify.localhost";
const PROJECTS = [
  { name: "Seed Project A", slug: "seed-project-a", owner: `owner-a@${DOMAIN}`, site: "a.seed.example.org" },
  { name: "Seed Project B", slug: "seed-project-b", owner: `owner-b@${DOMAIN}`, site: "b.seed.example.org" },
];
const OPERATOR = `operator@${DOMAIN}`;

const umamiUrl = (process.env.UMAMI_URL ?? "http://127.0.0.1:3001").replace(/\/$/, "");
const umamiKey = process.env.UMAMI_API_KEY;

const sql = postgres(process.env.DATABASE_URL ?? "postgresql://notify:notify@localhost:5432/notify", {
  max: 1,
  onnotice: () => {},
});
const hash = await hashPassword(password);

// Mesmo formato de src/lib/ingest-key.ts: ntf_<prefixo base32>_<segredo>.
function ingestKey() {
  const alphabet = "abcdefghijklmnopqrstuvwxyz234567";
  let bits = 0n;
  for (const byte of randomBytes(5)) bits = (bits << 8n) | BigInt(byte);
  let prefix = "";
  for (let i = 7; i >= 0; i--) prefix += alphabet[Number((bits >> BigInt(i * 5)) & 31n)];
  const key = `ntf_${prefix}_${randomBytes(32).toString("base64url")}`;
  return { prefix, keyHash: createHash("sha256").update(key).digest() };
}

async function ensureUser(tx, email, name, role) {
  const [existing] = await tx`select id from "user" where email = ${email}`;
  if (existing) return { id: existing.id, created: false };
  const id = randomUUID();
  await tx`insert into "user" (id, name, email, email_verified, role) values (${id}, ${name}, ${email}, true, ${role})`;
  await tx`insert into account (id, account_id, provider_id, user_id, password)
           values (${randomUUID()}, ${id}, 'credential', ${id}, ${hash})`;
  return { id, created: true };
}

async function createUmamiSite(name, domain) {
  const response = await fetch(`${umamiUrl}/api/websites`, {
    method: "POST",
    headers: { authorization: `Bearer ${umamiKey}`, "content-type": "application/json" },
    body: JSON.stringify({ name, domain }),
  });
  if (!response.ok) throw new Error(`Umami respondeu ${response.status} ao criar o site ${domain}`);
  return (await response.json()).id;
}

try {
  const operator = await sql.begin((tx) => ensureUser(tx, OPERATOR, "Seed Operator", "operator"));
  if (operator.created) {
    await sql`insert into audit_log (actor_user_id, action, details)
              values (null, 'operator.created', ${sql.json({ userId: operator.id })})`;
  }
  console.log(`${OPERATOR}: ${operator.created ? "criado" : "já existia"}`);

  for (const project of PROJECTS) {
    const [existing] = await sql`select id from tenant where slug = ${project.slug}`;
    let tenantId = existing?.id;
    if (!tenantId) {
      tenantId = await sql.begin(async (tx) => {
        const owner = await ensureUser(tx, project.owner, `${project.name} owner`, "tenant");
        const [created] = await tx`insert into tenant (name, slug) values (${project.name}, ${project.slug}) returning id`;
        await tx`insert into tenant_member (tenant_id, user_id, role) values (${created.id}, ${owner.id}, 'owner')`;
        await tx`insert into audit_log (actor_user_id, action, tenant_id, details)
                 values (${owner.id}, 'tenant.created', ${created.id}, ${sql.json({ slug: project.slug })})`;
        const [service] = await tx`insert into api_service (tenant_id, name) values (${created.id}, 'seed-api') returning id`;
        const { prefix, keyHash } = ingestKey();
        await tx`insert into ingest_key (api_service_id, tenant_id, prefix, key_hash, created_by)
                 values (${service.id}, ${created.id}, ${prefix}, ${keyHash}, ${owner.id})`;
        return created.id;
      });
      console.log(`${project.name}: criado, dono ${project.owner}`);
    } else {
      console.log(`${project.name}: já existia`);
    }

    const [site] = await sql`select id from site where tenant_id = ${tenantId}`;
    if (site) continue;
    if (!umamiKey) {
      console.log(`${project.name}: sem site (defina UMAMI_API_KEY para criar no Umami)`);
      continue;
    }
    const umamiWebsiteId = await createUmamiSite(`${project.name} site`, project.site);
    await sql`insert into site (tenant_id, name, domain, umami_website_id)
              values (${tenantId}, ${`${project.name} site`}, ${project.site}, ${umamiWebsiteId})`;
    console.log(`${project.name}: site ${project.site} criado no Umami`);
  }
} catch (error) {
  process.exitCode = 1;
  console.error(`Falha no seed: ${error.message}`);
} finally {
  await sql.end();
}
