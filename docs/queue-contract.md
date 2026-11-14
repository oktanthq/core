# Analysis queue contract

Core and Codehound share this contract. Core enqueues; Codehound consumes. Do not change field names or enums without updating both repos.

Codehound never receives HTTP calls from Core. It only reads pgmq messages, clones with the job token, writes `analysis_runs` / `findings` / `project_summaries`, and deletes the message.

This file matches `oktanthq/codehound` `docs/queue-contract.md` (commit `b780cc3`).

## Queue

|                     |                                                          |
| ------------------- | -------------------------------------------------------- |
| pgmq queue name     | `oktant_analysis` (override with `CODEHOUND_QUEUE_NAME`) |
| Event type consumed | `analysis.requested`                                     |
| Publisher           | Core                                                     |
| Consumer            | Codehound worker (`codehound worker`)                    |

The physical queue name is a Postgres identifier (`pgmq.q_<name>`). Use `oktant_analysis`, not `oktant.analysis`.

Create the queue with `supabase/migrations/20261114000400_pgmq_realtime.sql`.

Core inserts an `analysis_runs` row (`status = queued`) and `pgmq.send`s the message in **one Postgres transaction**. If send fails, the row is rolled back.

Phase 1 does not publish `analysis.started` / `analysis.completed` / `analysis.failed`. Codehound writes run status directly to `analysis_runs`.

## Envelope

Every pgmq `message` jsonb value is:

```json
{
  "event_id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
  "type": "analysis.requested",
  "version": 1,
  "occurred_at": "2026-11-13T19:00:00.000Z",
  "org_id": "11111111-1111-4111-8111-111111111111",
  "project_id": "22222222-2222-4222-8222-222222222222",
  "actor_user_id": "33333333-3333-4333-8333-333333333333",
  "correlation_id": "44444444-4444-4444-8444-444444444444",
  "payload": {}
}
```

| Field            | Type            | Required | Notes                                          |
| ---------------- | --------------- | -------- | ---------------------------------------------- |
| `event_id`       | UUID string     | yes      | Idempotency key for the event                  |
| `type`           | string          | yes      | Must be `analysis.requested` for this worker   |
| `version`        | number          | yes      | `1`                                            |
| `occurred_at`    | ISO-8601 string | yes      | When Core enqueued the request                 |
| `org_id`         | UUID string     | yes      | Must match `payload.org_id`                    |
| `project_id`     | UUID string     | yes      | Must match `payload.project_id`                |
| `actor_user_id`  | UUID string     | no       | User who clicked Run analysis, when known      |
| `correlation_id` | UUID string     | yes      | Ties Core HTTP request, queue message, and run |
| `payload`        | object          | yes      | See below                                      |

## Payload (`analysis.requested`)

```json
{
  "analysis_run_id": "55555555-5555-4555-8555-555555555555",
  "org_id": "11111111-1111-4111-8111-111111111111",
  "project_id": "22222222-2222-4222-8222-222222222222",
  "repository_id": "66666666-6666-4666-8666-666666666666",
  "provider": "github",
  "repo_full_name": "acme/web",
  "clone_url": "https://github.com/acme/web.git",
  "default_branch": "main",
  "commit_sha": "abc123def4567890abc123def4567890abc123de",
  "ref": "refs/heads/main",
  "trigger": "manual",
  "analysis_type": "security",
  "requested_analyzers": ["sast", "secrets", "dependencies"],
  "installation_id": 123456,
  "access": {
    "kind": "installation_token",
    "token": "ghs_not-a-real-token",
    "expires_at": "2026-11-13T20:00:00.000Z"
  }
}
```

| Field                 | Type            | Required | Notes                                                                                           |
| --------------------- | --------------- | -------- | ----------------------------------------------------------------------------------------------- |
| `analysis_run_id`     | UUID string     | yes      | Existing `analysis_runs.id` inserted by Core as `queued`                                        |
| `org_id`              | UUID string     | yes      | Tenant. Worker copies this from the job; never from the git remote                              |
| `project_id`          | UUID string     | yes      | Oktant project                                                                                  |
| `repository_id`       | UUID string     | yes      | `repositories.id`                                                                               |
| `provider`            | string          | yes      | `github` \| `gitlab` \| `bitbucket`                                                             |
| `repo_full_name`      | string          | yes      | `owner/name`                                                                                    |
| `clone_url`           | string          | yes      | HTTPS clone URL **without** credentials                                                         |
| `default_branch`      | string          | yes      | Used when `ref` is empty                                                                        |
| `commit_sha`          | string          | yes      | Full commit SHA. Core resolves HEAD if the user omitted it                                      |
| `ref`                 | string          | yes      | e.g. `refs/heads/main`                                                                          |
| `trigger`             | string          | yes      | `manual` \| `project_created` \| `push` \| `pull_request`                                       |
| `analysis_type`       | string          | yes      | `full` \| `security` \| `quality`                                                               |
| `requested_analyzers` | string[]        | yes      | Phase 1: `sast`, `secrets`, `dependencies`. Worker still runs `createDefaultSecurityPipeline()` |
| `installation_id`     | number          | yes      | GitHub App installation id (provider-specific later)                                            |
| `access`              | object          | yes      | Short-lived clone credential. Never logged                                                      |
| `access.kind`         | string          | yes      | `installation_token`                                                                            |
| `access.token`        | string          | yes      | Minted by Core. `contents:read`. ~1 hour                                                        |
| `access.expires_at`   | ISO-8601 string | yes      | Worker fails the run if the token is already expired                                            |

`clone_url` must not include the token. The worker injects `x-access-token` only in memory for `git clone` / `git fetch`.

## Delivery

Codehound calls `pgmq.read(queue_name, vt, qty)` with a visibility timeout (default 600 seconds) and `qty = 1`.

| Outcome                                                          | Queue                                                | `analysis_runs.status`                    |
| ---------------------------------------------------------------- | ---------------------------------------------------- | ----------------------------------------- |
| Success                                                          | `pgmq.delete`                                        | `completed`                               |
| Permanent error (invalid message, expired token, repo too large) | `pgmq.delete`                                        | `failed` (`error_code` / `error_message`) |
| Crash or retryable error before max attempts                     | left invisible until `vt` (or `pgmq.set_vt` backoff) | `running`                                 |
| `read_ct` greater than `CODEHOUND_MAX_ATTEMPTS`                  | `pgmq.delete`                                        | `failed` (`max_attempts`)                 |

Worktrees are always removed after the attempt, including failures.

## Results Codehound writes

Tables (service role / Postgres connection that bypasses RLS):

**`analysis_runs`** — `status` is `queued` (Core) then `running` \| `completed` \| `failed`. `error_code` and `error_message` on failure. Tokens never appear in `error_message`.

**`findings`** (one row per `VulnerabilityFinding`):

| Column                    | Source                                               |
| ------------------------- | ---------------------------------------------------- |
| `analysis_run_id`         | `payload.analysis_run_id`                            |
| `severity`                | `critical` \| `high` \| `medium` \| `low` \| `info`  |
| `title`                   | `title`                                              |
| `description`             | `message`                                            |
| `file_path`               | `path`                                               |
| `start_line` / `end_line` | `startLine` / `endLine`                              |
| `recommendation`          | remediation text from `suggestRemediations`          |
| `category`                | scanner category (`sast`, `secret`, `dependency`, …) |
| `rule_id`                 | `ruleId`                                             |
| `cwe`                     | `cwe` when present                                   |
| `evidence`                | `evidence` when present                              |
| `trace`                   | `trace` jsonb when present                           |

**`project_summaries`** — one repository-scope row summarizing finding counts.

Replay of the same `analysis_run_id` replaces findings and summaries before marking `completed`.

## Security

Do not log the envelope payload, `access.token`, or the GitHub App private key.
Core owns the App; Codehound only receives the job token.
