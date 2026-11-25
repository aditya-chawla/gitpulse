# backend/internal/scanner

Background repository scanner and snapshot persistence pipeline.

## Files

- `scanner.go` — Orchestrates scanning of both `owned` (GitHub App) and `watched` (public watchlist) repositories, enforcing rate-limit budgets and concurrency bounds.
- `persist.go` — Writes `repo_snapshots`, languages, branches, items, workflow runs, commits, `repo_trend_snapshots`, and upserts `action_items` while preserving user dismissals.
- `scanner_test.go` — Integration tests for scanning, watched-repo persistence, trend snapshot creation, and persistent action dismissal.
