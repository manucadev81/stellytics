// Cria um usuário comum (papel tenant) no banco `notify`, já com e-mail confirmado.
// Uso: pnpm --filter @notify/portal user:create -- --email a@b.c --name "Nome"
// A senha é lida da variável PORTAL_USER_PASSWORD (para não ficar no histórico do shell).
// Operador SCF só pelo comando de administração: scripts/operator.mjs (#19).
import { randomUUID } from "node:crypto";
import { parseArgs } from "node:util";
import { hashPassword } from "better-auth/crypto";
import postgres from "postgres";

const { values } = parseArgs({
  options: {
    email: { type: "string" },
    name: { type: "string" },
  },
});

const password = process.env.PORTAL_USER_PASSWORD;
if (!values.email || !values.name || !password) {
  console.error("Informe --email e --name, e a senha em PORTAL_USER_PASSWORD.");
  process.exit(1);
}
if (password.length < 12) {
  console.error("A senha precisa de pelo menos 12 caracteres.");
  process.exit(1);
}

const sql = postgres(process.env.DATABASE_URL ?? "postgresql://notify:notify@localhost:5432/notify", { max: 1 });
try {
  const userId = randomUUID();
  const hash = await hashPassword(password);
  await sql.begin(async (tx) => {
    await tx`insert into "user" (id, name, email, email_verified, role)
             values (${userId}, ${values.name}, ${values.email.toLowerCase()}, true, 'tenant')`;
    await tx`insert into account (id, account_id, provider_id, user_id, password)
             values (${randomUUID()}, ${userId}, 'credential', ${userId}, ${hash})`;
  });
  console.log(`Usuário ${values.email} criado.`);
} finally {
  await sql.end();
}
