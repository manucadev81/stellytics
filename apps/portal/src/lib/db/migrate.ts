import path from "node:path";
import { migrate } from "drizzle-orm/postgres-js/migrator";
import { db } from "./index";

// Aplica as migrações versionadas em ./drizzle. Usado na subida do servidor
// (instrumentation.ts) e nos testes de integração.
export async function runMigrations(migrationsFolder = path.join(process.cwd(), "drizzle")) {
  await migrate(db, { migrationsFolder });
}

// Contas de desenvolvimento (senhas conhecidas) em ./drizzle-dev, numa tabela de
// controle própria para não se misturar com a ordem das migrações reais. Só roda
// com SEED_DEV_DATA=true (Compose de dev); produção nunca define essa variável.
export async function runDevSeed(migrationsFolder = path.join(process.cwd(), "drizzle-dev")) {
  await migrate(db, { migrationsFolder, migrationsTable: "__dev_seed_migrations" });
}
