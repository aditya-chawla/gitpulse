# backend/internal/api

HTTP REST API layer built on `go-chi/chi`.

## Files

- `router.go` — Route registration (`/healthz`, `/readyz`, `/mcp`, `/api/*`), CORS, CSRF origin check middleware, and SPA static serving.
- `advisor.go` — `POST /api/agent/ask` handler for the context-aware AI advisor.
- `app.go` — `GET /api/app` GitHub App installation status and missing permission checks.
- `insights.go` — `GET /api/insights` and `GET /api/insights/{repoId}` Data Science endpoints.
- `profile.go` — Profile README settings, preview, and apply handlers.
- `prs.go` — GitHub webhook receiver (`/api/webhooks/github`), PR sync trigger, and PR review list/detail endpoints.
- `releases.go` — Release notes preview, draft creation, and draft history handlers.
- `scan.go` — `POST /api/scan`, `GET /api/scans/latest`, and `GET /api/scans/{id}` handlers.
- `stats.go` — Overview, analytics, repos (`?source=owned|watched`), repo detail, digest, and user settings handlers.
- `trends.go` — Trends overview, per-repo trend detail, action items (`?source=owned|watched`), and dismiss handlers.
- `watchlist.go` — `GET/POST /api/watchlist` and `DELETE /api/watchlist/{id}` handlers.
- `insights_test.go` — Integration tests for `/api/insights`, `/api/insights/{repoId}`, and `POST /api/agent/ask` against Postgres 17.
- `trends_test.go` — Integration tests for `/api/trends`, `/api/actions`, and action dismissal.
