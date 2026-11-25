# backend/internal

Private Go packages implementing `gitpulse`'s domain logic, GitHub integration, PostgreSQL persistence, Data Science analytics, AI advisor, and HTTP/MCP interfaces.

## Packages

- `advisor/` — Role-aware AI advisor (`maintainer` vs `contributor`), deterministic fallback, and BYOK clients (Gemini, OpenAI, Claude).
- `api/` — `chi` HTTP router, REST handlers, CORS, and CSRF origin checks.
- `auth/` — GitHub App OAuth sign-in, username allowlist, and cookie sessions.
- `config/` — Environment variable parsing and validation.
- `db/` — `pgxpool` connection setup and embedded `golang-migrate` SQL runner.
- `detect/` — Heuristic detection for README, license, CI workflows, tests, and tech stack.
- `github/` — GitHub GraphQL/REST client, ETag HTTP cache, and GitHub App JWT auth.
- `health/` — Pure 0–100 repository health scoring and activity status classification.
- `insights/` — Statistical/Data Science scoring (Z-score anomalies, PR responsiveness, contributor diversity, readiness).
- `install/` — GitHub App installation lookup and single-repo scoped token minting.
- `llm/` — Optional server-side Anthropic LLM client with monthly budget cap, caching, and untrusted input sanitization.
- `mcpserver/` — MCP tool definitions and streamable HTTP handler (`/mcp`).
- `profile/` — Profile README marked-block generator, Medium RSS fetcher, and commit-if-changed service.
- `prreview/` — PR assistant queue, deterministic code flags/labels, optional AI summary, and idempotent PR comments.
- `releases/` — Release note grouping since base tag, semver suggestion, and draft release creation.
- `scanner/` — Background scan orchestrator, watchlist scanning, and snapshot/trend/action persistence.
- `stats/` — Dashboard SQL queries, Postgres 17 DS aggregations (`REGR_SLOPE`, `PERCENTILE_CONT`), trends, and action items.
- `store/` — PostgreSQL CRUD for users, sessions, and per-repo automation settings.
- `trends/` — 7d/30d snapshot delta evaluator and silent decay alert rules.
- `watchlist/` — Public repository URL/slug validation and `watched_repos` CRUD.
