# core

[![CI](https://github.com/oktanthq/core/actions/workflows/ci.yml/badge.svg)](https://github.com/oktanthq/core/actions/workflows/ci.yml)

Core platform API for Oktant.

This service is the HTTP API in front of Oktant projects. The process boots from environment configuration, serves JSON over `net/http`, and keeps projects in memory until a database store is plugged into the same `project.Store` interface.

Authentication is not part of this scaffold. Put the API behind the platform gateway before exposing it.

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
curl -s -X POST localhost:8080/v1/projects \
  -H 'content-type: application/json' \
  -d '{"name":"Web Platform"}'
```

Build a binary with `make build`. The version string defaults to `dev` and can be set with `make build VERSION=1.2.3`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CORE_ADDR` | `:8080` | HTTP listen address |
| `CORE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `CORE_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown budget |

Logs are JSON on stderr. Each request line includes `method`, `path`, `status`, `duration`, and `request_id`.

## HTTP API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Process is up |
| `GET` | `/readyz` | Process can serve traffic |
| `GET` | `/v1/meta` | Service name and version |
| `GET` | `/v1/projects` | List projects |
| `POST` | `/v1/projects` | Create a project |
| `GET` | `/v1/projects/{id}` | Fetch one project |
| `PATCH` | `/v1/projects/{id}` | Update name, slug, or description |
| `DELETE` | `/v1/projects/{id}` | Delete a project |

`POST` and `PATCH` require `Content-Type: application/json`. Unknown JSON fields are rejected. A missing slug is derived from the name (`Web Platform` becomes `web-platform`). Slugs are unique.

Errors use one envelope:

```json
{"error":{"code":"invalid_request","message":"name is required"}}
```

`code` is `invalid_request`, `conflict`, `not_found`, `not_ready`, or `internal`.

## Layout

```
cmd/core              process entrypoint
internal/config       environment configuration
internal/project      project model and Store
internal/httpapi      routes, middleware, JSON responses
```

`project.Store` is the persistence seam. `project.MemoryStore` is what the server uses today. A Postgres implementation should satisfy the same interface and be passed to `httpapi.New`. Readiness can call `httpapi.ReadyCheck` so `/readyz` fails when that dependency is down.

## Development

```bash
make test
make vet
make build
```

CI runs `go build`, `go vet`, `go test -race`, and golangci-lint.
