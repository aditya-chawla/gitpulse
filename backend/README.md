# backend

Go 1.27 backend service for `gitpulse`: REST API, background GitHub scanner, PostgreSQL 17 migrations/queries, Data Science insights, role-aware AI advisor, and MCP server.

## Files

- `Dockerfile` — Multi-stage production Docker build (`golang:1.27-alpine` → `gcr.io/distroless/static-debian12:nonroot`) for the API server.
- `.dockerignore` — Excludes local env/build files from the Docker context.
- `.env.example` — Template of all supported environment variables for local development and deployment.
- `go.mod` / `go.sum` — Go module manifest and pinned dependency checksums.

## Subfolders

- `cmd/` — Executable entrypoints (`server`, `gitpulse-mcp`).
- `internal/` — Application packages, domain logic, database layer, and HTTP handlers.
