-- Escrita à mão (drizzle-kit generate --custom). Direito ao apagamento (#154).
--
-- Duas peças: apagar as métricas de API de um tenant de uma vez e permitir a
-- única mudança que o audit_log aceita depois de inserir — trocar
-- `actor_user_id` pelo marcador de apagamento, quando a conta é excluída.

-- Apaga todas as linhas de um tenant nas três tabelas de métricas, partição
-- por partição (o índice (tenant_id, bucket_start) serve a cada uma). É
-- stronger do que a retenção por idade (api_metric_drop_partitions), que
-- derruba partições inteiras: aqui a granularidade é o tenant. Quem chama é
-- o portal (dono das tabelas) no fluxo de apagamento.
CREATE FUNCTION api_metric_purge_tenant(target uuid) RETURNS void
LANGUAGE sql AS $$
  DELETE FROM api_metric_1m WHERE tenant_id = target;
  DELETE FROM api_metric_1h WHERE tenant_id = target;
  DELETE FROM api_metric_1d WHERE tenant_id = target;
$$;
--> statement-breakpoint
REVOKE EXECUTE ON FUNCTION api_metric_purge_tenant(uuid) FROM PUBLIC;
--> statement-breakpoint

-- O audit_log continua só-com-inserção para tudo, exceto uma mudança exata:
-- substituir um `actor_user_id` existente pelo marcador de apagamento, sem
-- tocar em qualquer outra coluna. É o que a exclusão de conta executa
-- (ANONYMIZED_ACTOR em src/lib/data/erasure.ts): as ações ficam no histórico,
-- mas deixam de apontar para a pessoa, cuja linha sai do banco. Um UPDATE
-- qualquer outro, DELETE e TRUNCATE seguem recusados, como na 0002.
CREATE FUNCTION audit_log_erasure_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE'
     AND NEW.id IS NOT DISTINCT FROM OLD.id
     AND NEW.at IS NOT DISTINCT FROM OLD.at
     AND NEW.action IS NOT DISTINCT FROM OLD.action
     AND NEW.tenant_id IS NOT DISTINCT FROM OLD.tenant_id
     AND NEW.details IS NOT DISTINCT FROM OLD.details
     AND OLD.actor_user_id IS NOT NULL
     AND NEW.actor_user_id = '[apagado]' THEN
    RETURN NEW;
  END IF;
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'audit_log só recebe inserções; a única mudança aceita troca actor_user_id pelo marcador de apagamento';
  END IF;
  RAISE EXCEPTION 'audit_log só recebe inserções';
END
$$;
--> statement-breakpoint
DROP TRIGGER audit_log_append_only ON "audit_log";
--> statement-breakpoint
-- O gatilho passa a ser por linha para poder comparar OLD e NEW. O de
-- TRUNCATE continua por comando com a função original da 0002.
CREATE TRIGGER audit_log_append_only
BEFORE UPDATE OR DELETE ON "audit_log"
FOR EACH ROW EXECUTE FUNCTION audit_log_erasure_guard();
