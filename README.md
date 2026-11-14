# core

[![CI](https://github.com/oktanthq/core/actions/workflows/ci.yml/badge.svg)](https://github.com/oktanthq/core/actions/workflows/ci.yml)

Core platform API for Oktant.

This service is the HTTP API in front of Oktant orgs, projects, the GitHub App,
and code analysis jobs. The process boots from environment configuration and
serves JSON over `net/http`. Scaffold project CRUD stays in memory. Org-scoped
projects, GitHub installs, and analysis runs persist in Supabase Postgres.

The GitHub App private key and installation tokens live only in Core. Scan jobs
are published on Supabase Queues (pgmq). Core never calls Codehound over HTTP.

## Requirements

Go 1.26.3 or later (see `go.mod`).

## Run

```bash
cp .env.example .env
go run ./cmd/core
```

The server listens on `:8080`. Stop it with Ctrl-C; in-flight requests are given `CORE_SHUTDOWN_TIMEOUT` to finish.

```bash
curl -s localhost:8080/healthz
curl -s localhost:8080/v1/meta
```

Build a binary with `make build`. The version string defaults to `dev` and can be set with `make build VERSION=1.2.3`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CORE_ADDR` | `:8080` | HTTP listen address |
| `CORE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `CORE_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown budget |
| `CORE_APP_ORIGIN` | | Web app origin allowed in GitHub callback redirects |
| `CORE_STATE_SECRET` | `SUPABASE_JWT_SECRET` | HMAC secret for GitHub connect `state` |
| `SUPABASE_URL` | | Project URL; JWT issuer becomes `{url}/auth/v1` |
| `SUPABASE_JWT_SECRET` | | HS256 secret for access tokens |
| `SUPABASE_SERVICE_ROLE_KEY` | | Service-role API key (not used on user request paths) |
| `DATABASE_URL` | | Postgres URL (service role / postgres). `SUPABASE_DB_URL` is an alias |
| `GITHUB_APP_ID` | | GitHub App id |
| `GITHUB_APP_PRIVATE_KEY` | | App PEM (PEM, `\n`-escaped PEM, or base64). Never log this |
| `GITHUB_APP_SLUG` | | Used to build the install URL |
| `GITHUB_APP_CALLBACK_URL` | | Public callback to set on the GitHub App |
| `GITHUB_API_URL` | `https://api.github.com` | GitHub API base |
| `GITHUB_WEB_URL` | `https://github.com` | GitHub web base for install and clone URLs |

Logs are JSON on stderr. Each request line includes `method`, `path`, `status`, `duration`, and `request_id`. Tokens, `Authorization` headers, and private keys are redacted.

Without `DATABASE_URL` and `SUPABASE_JWT_SECRET`, health/meta and in-memory `/v1/projects` still work; org, GitHub, and analysis routes return `503`.

## Database migrations

SQL lives in `supabase/migrations/`. **Do not apply these from this service to a live Supabase project.** Apply from a local CLI or the SQL editor you already use for that project:

```bash
supabase db reset          # local only
supabase db push           # linked project, when you intend to
psql "$DATABASE_URL" -f supabase/migrations/20261114000100_platform.sql
```

Order:

1. `20261114000100_platform.sql` — profiles, orgs, members, projects, integrations, repositories, signup trigger
2. `20261114000200_analysis.sql` — `analysis_runs`, `findings`, `project_summaries`
3. `20261114000300_rls.sql` — `is_org_member` and RLS
4. `20261114000400_pgmq_realtime.sql` — `pgmq` queue `oktant_analysis`, Realtime on the analysis tables

Enable the `pgmq` extension in the Supabase dashboard if `CREATE EXTENSION pgmq` is denied.

## HTTP API

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `GET` | `/healthz` | no | Process is up |
| `GET` | `/readyz` | no | Process can serve traffic |
| `GET` | `/v1/meta` | no | Service name and version |
| `GET` | `/v1/projects` | no | List in-memory scaffold projects |
| `POST` | `/v1/projects` | no | Create an in-memory scaffold project |
| `GET` | `/v1/projects/{id}` | no | Fetch one scaffold project |
| `PATCH` | `/v1/projects/{id}` | no | Update name, slug, or description |
| `DELETE` | `/v1/projects/{id}` | no | Delete a scaffold project |
| `POST` | `/v1/orgs/{orgId}/integrations/github/connect` | JWT, org admin | `{ redirect_to }` → `{ authorize_url }` |
| `GET` | `/v1/integrations/github/callback` | signed `state` | Stores the GitHub installation id, then redirects |
| `POST` | `/v1/orgs/{orgId}/projects` | JWT, org member | Persist project + repository |
| `POST` | `/v1/projects/{id}/analyses` | JWT, org member | Queue a scan (`202`) |

Protected routes expect `Authorization: Bearer <supabase access token>`.

`POST /v1/orgs/{orgId}/projects` body:

```json
{
  "name": "Web Platform",
  "slug": "web-platform",
  "description": "site",
  "integration_id": "uuid",
  "code_analysis": true,
  "repo": {
    "external_id": "123",
    "full_name": "acme/web",
    "default_branch": "main",
    "is_private": false
  }
}
```

`code_analysis` defaults to true and enqueues a `project_created` scan after insert.

`POST /v1/projects/{id}/analyses` body:

```json
{
  "repository_id": "uuid",
  "commit_sha": "abc123",
  "analysis_type": "security"
}
```

`analysis_type` is `full`, `security`, or `quality` (default `security`). Omit `commit_sha` to resolve `HEAD` of the repo default branch. A second request for the same repo + SHA + type while a run is `queued` or `running` returns the existing run.

The analysis handler inserts `analysis_runs` (`queued`) and `pgmq.send`s `analysis.requested` in one transaction, with a `contents:read` installation token in the payload. See [docs/queue-contract.md](docs/queue-contract.md).

`POST` and `PATCH` require `Content-Type: application/json`. Unknown JSON fields are rejected.

Errors use one envelope:

```json
{"error":{"code":"invalid_request","message":"name is required"}}
```

`code` is `invalid_request`, `unauthorized`, `forbidden`, `conflict`, `not_found`, `not_ready`, or `internal`.

Set the GitHub App callback / setup URL to `{CORE public origin}/v1/integrations/github/callback`.

## Layout

```
cmd/core              process entrypoint
internal/config       environment configuration
internal/auth         Supabase JWT verification
internal/githubapp    App JWT and installation tokens
internal/platform     org/project/analysis persistence
internal/queue        analysis.requested envelope
internal/project      in-memory scaffold project store
internal/httpapi      routes, middleware, JSON responses
supabase/migrations   Postgres / RLS / pgmq
docs/queue-contract.md
```

## Development

```bash
make test
make vet
make build
```

CI runs `go build`, `go vet`, `go test -race`, and golangci-lint.
