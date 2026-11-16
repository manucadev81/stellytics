// Roda uma vez na subida do servidor Next.js.
export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  if (process.env.RUN_MIGRATIONS !== "true") return;
  const { runMigrations, runDevSeed } = await import("./lib/db/migrate");
  await runMigrations();
  console.log("[portal] migrações aplicadas");
  if (process.env.SEED_DEV_DATA === "true") {
    await runDevSeed();
    console.log("[portal] contas de desenvolvimento aplicadas");
  }
}
