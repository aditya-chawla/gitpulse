# backend/internal/store

PostgreSQL persistence for users, sessions, and per-repo automation settings.

## Files

- `users.go` — User upsert/lookup and session token creation/validation/cleanup.
- `reposettings.go` — Per-repo PR assistant mode and draft-release toggles (forcing read-only defaults for `watched` repos).
- `reposettings_test.go` — Unit/integration tests for repo settings and watched-repo restrictions.
