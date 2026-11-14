-- Escrita à mão (drizzle-kit generate --custom). Spec do E5, seção 5 (#34).
--
-- Consolidação de api_metric_1m em api_metric_1h e api_metric_1d e retenção
-- por partição. Quem chama é o serviço services/rollup, como notify (dono das
-- tabelas), pela função api_metric_maintain().

-- Consolida as horas fechadas a partir de `since`: as que terminaram há pelo
-- menos 5 minutos antes de `until` (lotes atrasados do coletor). Depois refaz
-- os dias dessas horas a partir de api_metric_1h. O ON CONFLICT substitui os
-- valores pela soma recalculada, então rodar de novo sobre o mesmo período dá
-- o mesmo resultado. Tudo em UTC.
CREATE FUNCTION api_metric_rollup(since timestamptz, until timestamptz DEFAULT now()) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
  lo_hour timestamptz := date_trunc('hour', since, 'UTC');
  hi_hour timestamptz := date_trunc('hour', until - interval '5 minutes', 'UTC');
  lo_day timestamptz;
  hi_day timestamptz;
BEGIN
  IF lo_hour >= hi_hour THEN
    RETURN;
  END IF;

  INSERT INTO api_metric_1h AS m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                                  request_count, duration_sum, duration_buckets)
  SELECT date_trunc('hour', bucket_start, 'UTC'), tenant_id, api_service_id, route, method, status_code,
         sum(request_count), sum(duration_sum), api_metric_sum_buckets(duration_buckets)
  FROM api_metric_1m
  WHERE bucket_start >= lo_hour AND bucket_start < hi_hour
  GROUP BY 1, 2, 3, 4, 5, 6
  ON CONFLICT (tenant_id, api_service_id, route, method, status_code, bucket_start) DO UPDATE SET
    request_count = EXCLUDED.request_count,
    duration_sum = EXCLUDED.duration_sum,
    duration_buckets = EXCLUDED.duration_buckets;

  -- O dia corrente fica parcial até a última hora fechada e é refeito a cada execução.
  lo_day := date_trunc('day', lo_hour, 'UTC');
  hi_day := date_trunc('day', hi_hour - interval '1 hour', 'UTC') + interval '1 day';

  INSERT INTO api_metric_1d AS m (bucket_start, tenant_id, api_service_id, route, method, status_code,
                                  request_count, duration_sum, duration_buckets)
  SELECT date_trunc('day', bucket_start, 'UTC'), tenant_id, api_service_id, route, method, status_code,
         sum(request_count), sum(duration_sum), api_metric_sum_buckets(duration_buckets)
  FROM api_metric_1h
  WHERE bucket_start >= lo_day AND bucket_start < hi_day
  GROUP BY 1, 2, 3, 4, 5, 6
  ON CONFLICT (tenant_id, api_service_id, route, method, status_code, bucket_start) DO UPDATE SET
    request_count = EXCLUDED.request_count,
    duration_sum = EXCLUDED.duration_sum,
    duration_buckets = EXCLUDED.duration_buckets;
END
$$;
--> statement-breakpoint

-- Retenção (D3): apaga as partições mensais inteiras cujo fim ficou mais
-- antigo que 35 dias (api_metric_1m) ou 25 meses (api_metric_1h e
-- api_metric_1d). Nunca apaga linha a linha nem a partição default. Devolve
-- os nomes das partições apagadas.
CREATE FUNCTION api_metric_drop_partitions(at timestamptz DEFAULT now()) RETURNS SETOF text
LANGUAGE plpgsql AS $$
DECLARE
  r record;
  hi timestamptz;
  keep_after timestamptz;
BEGIN
  FOR r IN
    SELECT p.relname AS parent, c.relname AS child
    FROM pg_inherits i
    JOIN pg_class p ON p.oid = i.inhparent
    JOIN pg_class c ON c.oid = i.inhrelid
    WHERE p.relname IN ('api_metric_1m', 'api_metric_1h', 'api_metric_1d')
      AND c.relname ~ '_p[0-9]{6}$'
    ORDER BY c.relname
  LOOP
    hi := (to_date(right(r.child, 6), 'YYYYMM') + interval '1 month')::timestamp AT TIME ZONE 'UTC';
    keep_after := CASE WHEN r.parent = 'api_metric_1m' THEN at - interval '35 days' ELSE at - interval '25 months' END;
    IF hi <= keep_after THEN
      EXECUTE format('DROP TABLE %I', r.child);
      RETURN NEXT r.child;
    END IF;
  END LOOP;
END
$$;
--> statement-breakpoint

-- O que o serviço de consolidação roda a cada execução: partições futuras,
-- consolidação das horas fechadas em `lookback` e retenção.
CREATE FUNCTION api_metric_maintain(lookback interval DEFAULT interval '2 days') RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM api_metric_ensure_partitions(2);
  PERFORM api_metric_rollup(now() - lookback);
  PERFORM api_metric_drop_partitions();
END
$$;
--> statement-breakpoint
REVOKE EXECUTE ON FUNCTION api_metric_rollup(timestamptz, timestamptz) FROM PUBLIC;
--> statement-breakpoint
REVOKE EXECUTE ON FUNCTION api_metric_drop_partitions(timestamptz) FROM PUBLIC;
--> statement-breakpoint
REVOKE EXECUTE ON FUNCTION api_metric_maintain(interval) FROM PUBLIC;
