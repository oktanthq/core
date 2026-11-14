# Analysis queue contract

Core publishes scan jobs to Supabase Queues (pgmq). Codehound consumes them.
The HTTP API never calls Codehound. This file is the shared schema for both
services.

## Queue

| Item | Value |
| --- | --- |
| pgmq queue name | `oktant_analysis` |
| Event type | `analysis.requested` |
| Envelope version | `1` |
| Publisher | Core |
| Consumer | Codehound |

Create the queue with the SQL in `supabase/migrations/20261114000400_pgmq_realtime.sql`.

Core inserts an `analysis_runs` row (`status = queued`) and `pgmq.send`s the
message in **one Postgres transaction**. If send fails, the row is rolled back.

## Envelope

```json
{
  "event_id": "uuid",
  "type": "analysis.requested",
  "version": 1,
  "occurred_at": "2026-11-13T20:00:00Z",
  "org_id": "uuid",
  "project_id": "uuid",
  "actor_user_id": "uuid",
  "correlation_id": "request-id",
  "payload": {}
}
```

`actor_user_id` and `correlation_id` may be omitted. `occurred_at` is RFC 3339 UTC.

## Payload (`analysis.requested`)

```json
{
  "analysis_run_id": "uuid",
  "org_id": "uuid",
  "project_id": "uuid",
  "repository_id": "uuid",
  "provider": "github",
  "repo_full_name": "acme/web",
  "clone_url": "https://github.com/acme/web.git",
  "default_branch": "main",
  "commit_sha": "abc123",
  "ref": "refs/heads/main",
  "trigger": "manual",
  "analysis_type": "security",
  "requested_analyzers": ["sast", "secrets", "dependencies"],
  "installation_id": 123456,
  "access": {
    "kind": "installation_token",
    "token": "ghs_…",
    "expires_at": "2026-11-13T20:00:00Z"
  }
}
```

| Field | Notes |
| --- | --- |
| `trigger` | `manual` \| `project_created` \| `push` \| `pull_request` |
| `analysis_type` | `full` \| `security` \| `quality` |
| `requested_analyzers` | `security` → `sast`, `secrets`, `dependencies`; `quality` → `quality`; `full` → all of those |
| `provider` | `github` in Phase 1 |
| `access.kind` | `installation_token` |
| `access.token` | Short-lived GitHub installation token with `contents: read` for the one repo. Never log it. |
| `access.expires_at` | RFC 3339 UTC, typically one hour from mint |
| `commit_sha` | Required. Core resolves `HEAD` of `default_branch` when the caller omits it. |
| `installation_id` | GitHub App installation id stored on `integrations.external_account_id` |

## Consume

```sql
SELECT * FROM pgmq.read('oktant_analysis', 600, 1);
-- after success:
SELECT pgmq.delete('oktant_analysis', msg_id);
```

Visibility timeout should cover a full clone plus scan (ten minutes is a
reasonable start). Codehound should clone with
`https://x-access-token:<token>@github.com/<repo_full_name>.git` at `commit_sha`,
write `findings` / `project_summaries` with the service role, and set
`analysis_runs.status` to `running` then `completed` or `failed`.

## Security

Do not log the envelope payload, `access.token`, or the GitHub App private key.
Core owns the App; Codehound only receives the job token.
