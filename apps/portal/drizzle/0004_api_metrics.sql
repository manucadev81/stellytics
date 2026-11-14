-- Escrita à mão (drizzle-kit generate --custom). Spec do E5, seção 4 (#33).
--
-- Métricas de API: api_metric_1m (gravada pelo coletor), api_metric_1h e
-- api_metric_1d (consolidadas pelo job da #34). As três têm as mesmas colunas,
-- são particionadas por mês em bucket_start e têm uma partição default, para
-- nunca recusar um ponto.
--
-- duration_buckets conta as durações nos 15 buckets do spec (D2), com os
-- limites superiores, em segundos, da recomendação do OpenTelemetry para
-- http.server.request.duration e um último bucket acima de 10 s.
DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['api_metric_1m', 'api_metric_1h', 'api_metric_1d'] LOOP
    EXECUTE format($sql$
      CREATE TABLE %1$I (
        bucket_start timestamptz NOT NULL,
        tenant_id uuid NOT NULL,
        api_service_id uuid NOT NULL,
        route text NOT NULL CHECK (octet_length(route) BETWEEN 1 AND 256),
        method text NOT NULL CHECK (octet_length(method) BETWEEN 1 AND 16),
        status_code smallint NOT NULL CHECK (status_code BETWEEN 0 AND 999),
        request_count bigint NOT NULL CHECK (request_count >= 0),
        duration_sum double precision NOT NULL CHECK (duration_sum >= 0),
        duration_buckets bigint[] NOT NULL CHECK (cardinality(duration_buckets) = 15),
        PRIMARY KEY (tenant_id, api_service_id, route, method, status_code, bucket_start)
      ) PARTITION BY RANGE (bucket_start)$sql$, t);
    EXECUTE format('CREATE TABLE %I PARTITION OF %I DEFAULT', t || '_default', t);
    -- Painéis: o período de um tenant, filtrado depois por serviço e rota.
    EXECUTE format('CREATE INDEX %I ON %I (tenant_id, bucket_start)', t || '_tenant_time', t);
  END LOOP;
END
$$;
--> statement-breakpoint

-- Limites superiores dos buckets de duração, em segundos (spec, D2).
CREATE FUNCTION api_metric_bounds() RETURNS double precision[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN ARRAY[0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10]::double precision[];
--> statement-breakpoint

-- Soma dois arrays de buckets posição a posição. Usada no ON CONFLICT do
-- coletor e no agregado abaixo.
CREATE FUNCTION api_metric_add_buckets(a bigint[], b bigint[]) RETURNS bigint[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE
  WHEN a IS NULL THEN b
  WHEN b IS NULL THEN a
  ELSE ARRAY(SELECT x + y FROM unnest(a, b) AS u(x, y))
END;
--> statement-breakpoint

-- Soma os buckets de várias linhas: api_metric_sum_buckets(duration_buckets).
CREATE AGGREGATE api_metric_sum_buckets(bigint[]) (
  SFUNC = api_metric_add_buckets,
  STYPE = bigint[],
  PARALLEL = SAFE
);
--> statement-breakpoint

-- Percentil q (0 a 1) a partir dos buckets, interpolando linearmente dentro do
-- bucket onde ele cai. No último bucket (acima de 10 s) devolve 10. Nulo sem
-- requisições.
CREATE FUNCTION api_metric_quantile(buckets bigint[], q double precision) RETURNS double precision
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
  bounds double precision[] := api_metric_bounds();
  total bigint;
  target double precision;
  acc bigint := 0;
  lower double precision := 0;
BEGIN
  IF buckets IS NULL OR q IS NULL OR q < 0 OR q > 1 THEN
    RETURN NULL;
  END IF;
  SELECT sum(b) INTO total FROM unnest(buckets) AS b;
  IF total IS NULL OR total = 0 THEN
    RETURN NULL;
  END IF;
  target := q * total;
  FOR i IN 1 .. cardinality(buckets) LOOP
    IF buckets[i] > 0 AND acc + buckets[i] >= target THEN
      IF i > cardinality(bounds) THEN
        RETURN bounds[cardinality(bounds)];
      END IF;
      RETURN lower + (bounds[i] - lower) * (target - acc) / buckets[i];
    END IF;
    acc := acc + buckets[i];
    IF i <= cardinality(bounds) THEN
      lower := bounds[i];
    END IF;
  END LOOP;
  RETURN bounds[cardinality(bounds)];
END
$$;
--> statement-breakpoint

-- Cria as partições mensais (UTC) do mês corrente e dos `months` seguintes nas
-- três tabelas. Se a partição default já tiver linhas do mês (o job atrasou),
-- elas são movidas para a partição nova. Idempotente; o job da #34 chama todo dia.
CREATE FUNCTION api_metric_ensure_partitions(months integer DEFAULT 2) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
  t text;
  part text;
  lo timestamptz;
  hi timestamptz;
BEGIN
  FOREACH t IN ARRAY ARRAY['api_metric_1m', 'api_metric_1h', 'api_metric_1d'] LOOP
    FOR m IN 0 .. months LOOP
      lo := (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => m)) AT TIME ZONE 'UTC';
      hi := (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => m + 1)) AT TIME ZONE 'UTC';
      part := t || '_p' || to_char(lo AT TIME ZONE 'UTC', 'YYYYMM');
      CONTINUE WHEN to_regclass(part) IS NOT NULL;
      EXECUTE format('CREATE TABLE %I (LIKE %I INCLUDING DEFAULTS INCLUDING CONSTRAINTS)', part, t);
      EXECUTE format(
        'WITH moved AS (DELETE FROM %I WHERE bucket_start >= $1 AND bucket_start < $2 RETURNING *) '
        'INSERT INTO %I SELECT * FROM moved', t || '_default', part) USING lo, hi;
      EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (%L) TO (%L)', t, part, lo, hi);
    END LOOP;
  END LOOP;
END
$$;
--> statement-breakpoint
SELECT api_metric_ensure_partitions(2);
--> statement-breakpoint

-- Permissões (spec, seção 4). O coletor só grava o balde de 1 minuto, pelo
-- INSERT ... ON CONFLICT DO UPDATE, que exige também SELECT nas colunas lidas.
-- O consolidado e a manutenção são do dono (notify).
REVOKE ALL ON api_metric_1m, api_metric_1h, api_metric_1d FROM PUBLIC;
--> statement-breakpoint
GRANT SELECT, INSERT, UPDATE ON api_metric_1m TO notify_collector;
--> statement-breakpoint
REVOKE EXECUTE ON FUNCTION api_metric_ensure_partitions(integer) FROM PUBLIC;
