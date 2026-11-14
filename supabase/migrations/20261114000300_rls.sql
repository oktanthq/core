-- RLS helper and policies. Core and Codehound connect with a role that
-- bypasses RLS (service_role or postgres). Browser reads use authenticated.

CREATE OR REPLACE FUNCTION public.org_role_rank(role text)
RETURNS integer
LANGUAGE sql
IMMUTABLE
AS $$
  SELECT CASE role
    WHEN 'member' THEN 1
    WHEN 'admin' THEN 2
    WHEN 'owner' THEN 3
    ELSE 0
  END
$$;

CREATE OR REPLACE FUNCTION public.is_org_member(org_id uuid, min_role text DEFAULT 'member')
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public
AS $$
  SELECT EXISTS (
    SELECT 1
    FROM public.organization_members m
    WHERE m.organization_id = org_id
      AND m.user_id = auth.uid()
      AND public.org_role_rank(m.role) >= public.org_role_rank(min_role)
  )
$$;

REVOKE ALL ON FUNCTION public.org_role_rank(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.is_org_member(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.org_role_rank(text) TO authenticated;
GRANT EXECUTE ON FUNCTION public.is_org_member(uuid, text) TO authenticated;

ALTER TABLE public.user_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.organization_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.integrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.repositories ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.analysis_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.project_summaries ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_profiles_select_own
  ON public.user_profiles FOR SELECT TO authenticated
  USING (user_id = auth.uid());

CREATE POLICY user_profiles_update_own
  ON public.user_profiles FOR UPDATE TO authenticated
  USING (user_id = auth.uid())
  WITH CHECK (user_id = auth.uid());

CREATE POLICY organizations_select_member
  ON public.organizations FOR SELECT TO authenticated
  USING (public.is_org_member(id, 'member'));

CREATE POLICY organization_members_select
  ON public.organization_members FOR SELECT TO authenticated
  USING (user_id = auth.uid() OR public.is_org_member(organization_id, 'member'));

CREATE POLICY projects_select_member
  ON public.projects FOR SELECT TO authenticated
  USING (public.is_org_member(organization_id, 'member'));

CREATE POLICY projects_insert_member
  ON public.projects FOR INSERT TO authenticated
  WITH CHECK (public.is_org_member(organization_id, 'member'));

CREATE POLICY projects_update_admin
  ON public.projects FOR UPDATE TO authenticated
  USING (public.is_org_member(organization_id, 'admin'))
  WITH CHECK (public.is_org_member(organization_id, 'admin'));

CREATE POLICY integrations_select_member
  ON public.integrations FOR SELECT TO authenticated
  USING (public.is_org_member(organization_id, 'member'));

CREATE POLICY integrations_write_admin
  ON public.integrations FOR INSERT TO authenticated
  WITH CHECK (public.is_org_member(organization_id, 'admin'));

CREATE POLICY integrations_update_admin
  ON public.integrations FOR UPDATE TO authenticated
  USING (public.is_org_member(organization_id, 'admin'))
  WITH CHECK (public.is_org_member(organization_id, 'admin'));

CREATE POLICY repositories_select_member
  ON public.repositories FOR SELECT TO authenticated
  USING (
    EXISTS (
      SELECT 1
      FROM public.projects p
      WHERE p.id = repositories.project_id
        AND public.is_org_member(p.organization_id, 'member')
    )
  );

CREATE POLICY repositories_insert_member
  ON public.repositories FOR INSERT TO authenticated
  WITH CHECK (
    EXISTS (
      SELECT 1
      FROM public.projects p
      WHERE p.id = repositories.project_id
        AND public.is_org_member(p.organization_id, 'member')
    )
  );

CREATE POLICY analysis_runs_select_member
  ON public.analysis_runs FOR SELECT TO authenticated
  USING (
    EXISTS (
      SELECT 1
      FROM public.repositories r
      JOIN public.projects p ON p.id = r.project_id
      WHERE r.id = analysis_runs.repository_id
        AND public.is_org_member(p.organization_id, 'member')
    )
  );

CREATE POLICY findings_select_member
  ON public.findings FOR SELECT TO authenticated
  USING (
    EXISTS (
      SELECT 1
      FROM public.analysis_runs ar
      JOIN public.repositories r ON r.id = ar.repository_id
      JOIN public.projects p ON p.id = r.project_id
      WHERE ar.id = findings.analysis_run_id
        AND public.is_org_member(p.organization_id, 'member')
    )
  );

CREATE POLICY project_summaries_select_member
  ON public.project_summaries FOR SELECT TO authenticated
  USING (
    EXISTS (
      SELECT 1
      FROM public.analysis_runs ar
      JOIN public.repositories r ON r.id = ar.repository_id
      JOIN public.projects p ON p.id = r.project_id
      WHERE ar.id = project_summaries.analysis_run_id
        AND public.is_org_member(p.organization_id, 'member')
    )
  );

GRANT USAGE ON SCHEMA public TO authenticated;
GRANT SELECT ON public.user_profiles, public.organizations, public.organization_members,
  public.projects, public.integrations, public.repositories, public.analysis_runs,
  public.findings, public.project_summaries TO authenticated;
GRANT INSERT, UPDATE ON public.projects, public.integrations, public.repositories TO authenticated;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.user_profiles, public.organizations,
  public.organization_members, public.projects, public.integrations, public.repositories,
  public.analysis_runs, public.findings, public.project_summaries TO service_role;
