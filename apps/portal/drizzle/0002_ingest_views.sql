-- Escrita à mão (drizzle-kit generate --custom). Spec do E2, seções 3 e 4 (D4).
--
-- Visões lidas pelo gateway web e pelo coletor, com permissão mínima para os
-- usuários notify_ingest e notify_collector. Os dois usuários são criados fora
-- das migrações, por quem administra o servidor (infra/compose/postgres/init/
-- 02-ingest-roles.sh no ambiente local), porque criar papel exige superusuário.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'notify_ingest')
     OR NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'notify_collector') THEN
    RAISE EXCEPTION 'Os papéis notify_ingest e notify_collector precisam existir antes desta migração. Rode infra/compose/postgres/init/02-ingest-roles.sh.';
  END IF;
END
$$;
--> statement-breakpoint

-- Gateway web (#61): o ID do site no Umami e se ele aceita ingestão agora.
-- A visão roda com as permissões do dono, então o leitor não precisa de acesso
-- às tabelas.
CREATE VIEW "ingest_site" AS
SELECT
  s.umami_website_id,
  (s.archived_at IS NULL AND t.status = 'active') AS accepts_ingest
FROM site s
JOIN tenant t ON t.id = s.tenant_id;
--> statement-breakpoint

-- Coletor (#30): só as chaves válidas agora (não revogadas, serviço não
-- arquivado, tenant ativo). O coletor busca pelo prefixo e compara o hash.
CREATE VIEW "ingest_key_active" AS
SELECT
  k.prefix,
  k.key_hash,
  k.tenant_id,
  k.api_service_id,
  s.name AS service_name
FROM ingest_key k
JOIN api_service s ON s.id = k.api_service_id
JOIN tenant t ON t.id = k.tenant_id
WHERE k.revoked_at IS NULL
  AND s.archived_at IS NULL
  AND t.status = 'active';
--> statement-breakpoint

REVOKE ALL ON "ingest_site", "ingest_key_active" FROM PUBLIC;
--> statement-breakpoint
GRANT SELECT ON "ingest_site" TO notify_ingest;
--> statement-breakpoint
-- O INSERT nas tabelas de métricas entra no E5, junto com as tabelas.
GRANT SELECT ON "ingest_key_active" TO notify_collector;
--> statement-breakpoint

-- audit_log só recebe inserções.
CREATE FUNCTION audit_log_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_log só recebe inserções';
END
$$;
--> statement-breakpoint
CREATE TRIGGER audit_log_append_only
BEFORE UPDATE OR DELETE ON "audit_log"
FOR EACH STATEMENT EXECUTE FUNCTION audit_log_append_only();
--> statement-breakpoint
CREATE TRIGGER audit_log_no_truncate
BEFORE TRUNCATE ON "audit_log"
FOR EACH STATEMENT EXECUTE FUNCTION audit_log_append_only();
