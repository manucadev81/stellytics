// Comando de administração dos operadores SCF (#19, spec do E2, D2 e D3).
// Só quem tem acesso ao servidor (e ao banco) cria, promove ou rebaixa
// operador; o portal não tem tela para isso.
//
// Uso:
//   read -rs OPERATOR_PASSWORD && export OPERATOR_PASSWORD
//   pnpm operator:create  --email scf@example.org --name "Nome"
//   pnpm operator:promote --email pessoa@example.org
//   pnpm operator:demote  --email pessoa@example.org
//
// - create: conta nova, já com e-mail confirmado. Senha em OPERATOR_PASSWORD.
// - promote: conta existente que não é membro de tenant (D2: operador não tem
//   projeto; quem é do SCF e tem um projeto usa duas contas).
// - demote: volta o operador a usuário comum.
// Toda mudança vai para o audit_log (ator nulo = script) e encerra as sessões
// da conta, para o papel novo valer já no próximo pedido.
import { randomUUID } from "node:crypto";
import { parseArgs } from "node:util";
import { hashPassword } from "better-auth/crypto";
import postgres from "postgres";

const [command, ...rest] = process.argv.slice(2);
const { values } = parseArgs({ args: rest, options: { email: { type: "string" }, name: { type: "string" } } });

function fail(message) {
  console.error(message);
  process.exit(1);
}

// Erro de uso dentro da transação: desfaz tudo e mostra só a mensagem.
class Refused extends Error {}

if (!["create", "promote", "demote"].includes(command)) fail("Use: operator.mjs create|promote|demote --email ...");
if (!values.email) fail("Informe --email.");
const email = values.email.trim().toLowerCase();

const sql = postgres(process.env.DATABASE_URL ?? "postgresql://notify:notify@localhost:5432/notify", {
  max: 1,
  onnotice: () => {},
});

async function audit(tx, action, userId) {
  await tx`insert into audit_log (actor_user_id, action, details)
           values (null, ${action}, ${sql.json({ userId })})`;
}

try {
  if (command === "create") {
    const password = process.env.OPERATOR_PASSWORD;
    if (!values.name || !password) fail("Informe --name, e a senha em OPERATOR_PASSWORD.");
    if (password.length < 12) fail("A senha precisa de pelo menos 12 caracteres.");
    const userId = randomUUID();
    const hash = await hashPassword(password);
    await sql.begin(async (tx) => {
      await tx`insert into "user" (id, name, email, email_verified, role)
               values (${userId}, ${values.name}, ${email}, true, 'operator')`;
      await tx`insert into account (id, account_id, provider_id, user_id, password)
               values (${randomUUID()}, ${userId}, 'credential', ${userId}, ${hash})`;
      await audit(tx, "operator.created", userId);
    });
    console.log(`Operador ${email} criado.`);
  } else {
    await sql.begin(async (tx) => {
      const [found] = await tx`select id, role from "user" where email = ${email} for update`;
      if (!found) throw new Refused(`Nenhuma conta com o e-mail ${email}.`);
      if (command === "promote") {
        if (found.role === "operator") throw new Refused(`${email} já é operador.`);
        const [member] = await tx`select 1 from tenant_member where user_id = ${found.id}`;
        if (member) throw new Refused(`${email} é membro de um projeto e não pode ser operador (D2). Use outra conta.`);
        await tx`update "user" set role = 'operator', updated_at = now() where id = ${found.id}`;
        await audit(tx, "operator.promoted", found.id);
      } else {
        if (found.role !== "operator") throw new Refused(`${email} não é operador.`);
        await tx`update "user" set role = 'tenant', updated_at = now() where id = ${found.id}`;
        await audit(tx, "operator.demoted", found.id);
      }
      await tx`delete from session where user_id = ${found.id}`;
    });
    console.log(command === "promote" ? `${email} agora é operador.` : `${email} deixou de ser operador.`);
  }
} catch (error) {
  process.exitCode = 1;
  // Sem a senha nem o hash na mensagem.
  if (error instanceof Refused) console.error(error.message);
  else console.error(`Falha: ${error.code === "23505" ? "já existe uma conta com esse e-mail" : error.message}`);
} finally {
  await sql.end();
}
