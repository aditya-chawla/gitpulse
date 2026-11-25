# backend/internal/github

GitHub GraphQL and REST API client with ETag caching, rate-limit backoff, and GitHub App JWT authentication.

## Files

- `app.go` — RSA private key parsing and GitHub App JWT generation / installation token exchange.
- `client.go` — `Client` interface and HTTP client with ETag cache (`http_cache`), rate-limit handling, and retries.
- `queries.go` — GraphQL and REST queries for owned repos, public watched repos, branches, PRs/issues, workflow runs, and recent commit author sampling.
- `pulls.go` — Pull request files, diff stats, commits, and comment upsert helpers for the PR assistant.
- `releases.go` — Tags, merged PRs, and draft release creation helpers.
- `types.go` — Shared GitHub domain structs (`Repo`, `Branch`, `Item`, `WorkflowRun`, `Commit`, `Release`, etc.).
- `app_test.go` — Unit tests for JWT signing and private key parsing.
- `client_test.go` — Unit tests for ETag caching, rate-limit handling, and GraphQL/REST queries.
