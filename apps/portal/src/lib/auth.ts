import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { nextCookies } from "better-auth/next-js";
import { db } from "./db";
import * as schema from "./db/schema";
import { env } from "./env";
import { resetPasswordEmail, sendMail, verificationEmail } from "./mail";

export const auth = betterAuth({
  secret: env.authSecret,
  baseURL: env.authUrl,
  database: drizzleAdapter(db, { provider: "pg", schema }),
  emailAndPassword: {
    enabled: true,
    minPasswordLength: 12,
    // Cadastro aberto (#60): a conta só entra depois de confirmar o e-mail,
    // então não cria projeto antes disso. Com esta opção, o cadastro com um
    // e-mail que já existe recebe a mesma resposta de um cadastro novo, sem
    // revelar quem tem conta.
    requireEmailVerification: true,
    sendResetPassword: async ({ user, url }) => {
      await sendMail({ to: user.email, ...resetPasswordEmail(url) });
    },
    // Link de redefinição vale 30 minutos e uma vez só.
    resetPasswordTokenExpiresIn: 60 * 30,
    // Redefinir a senha encerra as outras sessões.
    revokeSessionsOnPasswordReset: true,
  },
  emailVerification: {
    sendVerificationEmail: async ({ user, url }) => {
      await sendMail({ to: user.email, ...verificationEmail(url) });
    },
    sendOnSignUp: true,
    // Quem tenta entrar sem ter confirmado recebe outro link.
    sendOnSignIn: true,
    autoSignInAfterVerification: true,
    expiresIn: 60 * 60 * 24,
  },
  user: {
    additionalFields: {
      // input: false impede que o cliente defina o papel.
      role: { type: "string", required: false, defaultValue: "tenant", input: false },
    },
  },
  session: {
    expiresIn: 60 * 60 * 24 * 7, // 7 dias
    updateAge: 60 * 60 * 24, // renova a cada 1 dia de uso
  },
  // Limite de tentativas por IP (em memória, por instância). O IP é usado só
  // para isso. O Better Auth já limita entrar a 3 pedidos a cada 10 segundos.
  // Para cadastro e pedidos que enviam e-mail, as regras abaixo trocam o
  // limite padrão por um limite por hora, mais apertado (#60).
  rateLimit: {
    enabled: true,
    window: 60,
    max: 30,
    customRules: {
      "/sign-up/email": { window: 60 * 60, max: 10 },
      "/request-password-reset": { window: 60 * 60, max: 5 },
      "/send-verification-email": { window: 60 * 60, max: 5 },
    },
  },
  // "warn" é o padrão; fica explícito porque o nível "info" registra o e-mail
  // de quem tenta se cadastrar de novo.
  logger: { level: "warn" },
  databaseHooks: {
    session: {
      create: {
        // Não persiste o IP de quem entra no portal.
        before: async (data) => ({ data: { ...data, ipAddress: null } }),
      },
    },
  },
  telemetry: { enabled: false },
  plugins: [nextCookies()],
});

export type Session = typeof auth.$Infer.Session;
