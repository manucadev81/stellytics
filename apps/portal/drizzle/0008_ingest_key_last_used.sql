-- Escrita à mão (drizzle-kit generate --custom). Registra o último uso de uma
-- chave de ingestão (ingest_key.last_used_on).
--
-- Quem autentica a chave é o coletor (notify_collector), que só lê a visão
-- ingest_key_active e não tem acesso à tabela. Em vez de dar UPDATE por coluna
-- na tabela (o que exigiria também SELECT em prefix, revoked_at e demais
-- colunas para o WHERE, abrindo a leitura de chaves revogadas e de key_hash),
-- o coletor ganha só EXECUTE nesta função. Ela roda como dono (notify), grava
-- uma única coluna, e só para uma chave que a visão ingest_key_active considera
-- ativa (não revogada, serviço não arquivado, tenant ativo). O dia é o dia UTC
-- do banco, e a função não escreve de novo se a data já é a de hoje.
CREATE OR REPLACE FUNCTION public.ingest_key_touch(p_prefix text) RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  updated integer;
BEGIN
  UPDATE public.ingest_key k
  SET last_used_on = (now() AT TIME ZONE 'UTC')::date
  WHERE k.prefix = p_prefix
    AND k.last_used_on IS DISTINCT FROM (now() AT TIME ZONE 'UTC')::date
    AND EXISTS (SELECT 1 FROM public.ingest_key_active a WHERE a.prefix = k.prefix);
  GET DIAGNOSTICS updated = ROW_COUNT;
  RETURN updated > 0;
END
$$;
--> statement-breakpoint
REVOKE ALL ON FUNCTION public.ingest_key_touch(text) FROM PUBLIC;
--> statement-breakpoint
GRANT EXECUTE ON FUNCTION public.ingest_key_touch(text) TO notify_collector;
