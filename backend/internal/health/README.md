# backend/internal/health

Pure 0–100 repository health scoring and activity status classification.

## Files

- `health.go` — Computes the 0–100 health score, per-check point breakdown (README, license, `.gitignore`, CI, tests, recent push, stale branches, old PRs, description/topics), and `active` / `slowing` / `dormant` status.
- `health_test.go` — Unit tests for health scoring rules and status thresholds.
