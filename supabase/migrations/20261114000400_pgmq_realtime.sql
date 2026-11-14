-- Analysis job queue (pgmq) and Realtime on the tables the web app watches.
-- Enable the pgmq extension in the Supabase project if CREATE EXTENSION fails.

CREATE EXTENSION IF NOT EXISTS pgmq;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_catalog.pg_namespace n
    JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid
    WHERE n.nspname = 'pgmq' AND c.relname = 'meta'
  ) THEN
    IF NOT EXISTS (SELECT 1 FROM pgmq.meta WHERE queue_name = 'oktant_analysis') THEN
      PERFORM pgmq.create('oktant_analysis');
    END IF;
  ELSE
    PERFORM pgmq.create('oktant_analysis');
  END IF;
END
$$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    CREATE PUBLICATION supabase_realtime;
  END IF;
END
$$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE public.analysis_runs;
EXCEPTION
  WHEN duplicate_object THEN NULL;
END
$$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE public.findings;
EXCEPTION
  WHEN duplicate_object THEN NULL;
END
$$;

DO $$
BEGIN
  ALTER PUBLICATION supabase_realtime ADD TABLE public.project_summaries;
EXCEPTION
  WHEN duplicate_object THEN NULL;
END
$$;

ALTER TABLE public.analysis_runs REPLICA IDENTITY FULL;
ALTER TABLE public.findings REPLICA IDENTITY FULL;
ALTER TABLE public.project_summaries REPLICA IDENTITY FULL;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'service_role') THEN
    GRANT USAGE ON SCHEMA pgmq TO service_role;
    GRANT ALL ON ALL TABLES IN SCHEMA pgmq TO service_role;
    GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA pgmq TO service_role;
  END IF;
END
$$;
