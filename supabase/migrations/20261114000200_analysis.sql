-- Analysis tables the frontend already reads and Codehound writes.
-- Column names match src/lib/projects/page-data.ts in the web app.

CREATE TABLE public.analysis_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  repository_id uuid NOT NULL REFERENCES public.repositories (id) ON DELETE CASCADE,
  analysis_type text NOT NULL CHECK (analysis_type IN ('full', 'security', 'quality')),
  status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
  commit_sha text NOT NULL,
  trigger text NOT NULL CHECK (trigger IN ('manual', 'project_created', 'push', 'pull_request')),
  requested_analyzers jsonb NOT NULL DEFAULT '[]'::jsonb,
  started_at timestamptz,
  completed_at timestamptz,
  error_code text,
  error_message text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX analysis_runs_repository_id_idx
  ON public.analysis_runs (repository_id);

CREATE INDEX analysis_runs_created_at_idx
  ON public.analysis_runs (created_at DESC);

CREATE UNIQUE INDEX analysis_runs_active_dedup
  ON public.analysis_runs (repository_id, commit_sha, analysis_type)
  WHERE status IN ('queued', 'running');

CREATE TABLE public.findings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  analysis_run_id uuid NOT NULL REFERENCES public.analysis_runs (id) ON DELETE CASCADE,
  severity text NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'info')),
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  file_path text,
  start_line integer,
  end_line integer,
  recommendation text,
  category text NOT NULL DEFAULT '',
  rule_id text,
  cwe text,
  evidence text,
  trace jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX findings_analysis_run_id_idx
  ON public.findings (analysis_run_id);

CREATE TABLE public.project_summaries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  analysis_run_id uuid NOT NULL REFERENCES public.analysis_runs (id) ON DELETE CASCADE,
  scope text NOT NULL DEFAULT 'repository',
  target_path text,
  summary text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX project_summaries_analysis_run_id_idx
  ON public.project_summaries (analysis_run_id);
