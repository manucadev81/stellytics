-- Escrita à mão (drizzle-kit generate --custom). #48, ADR 0006.
--
-- operator_api_key guarda o hash das chaves de leitura do SCF. Só o dono (o
-- usuário do portal) lê e grava. O gateway (stellytics_ingest) e o coletor
-- (stellytics_collector) não podem ver a tabela: uma chave de leitura nunca vale
-- na ingestão, e a ingestão nunca enxerga as chaves do SCF.
REVOKE ALL ON "operator_api_key" FROM PUBLIC;
--> statement-breakpoint
REVOKE ALL ON "operator_api_key" FROM stellytics_ingest, stellytics_collector;
