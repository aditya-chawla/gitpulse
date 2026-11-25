# backend/internal/trends

Deterministic 7-day / 30-day trend evaluation and silent decay rules.

## Files

- `trends.go` — Compares current snapshots against 7d/30d baselines to compute `improving` / `steady` / `slipping` direction and evaluate decay rules (`pr_bottleneck`, `stale_branches`, `ci_rot`, `commit_cadence_drop`, `bus_factor`, `unprotected_code`, `no_recent_release`).
- `trends_test.go` — Unit tests for delta calculations, direction classification, and every decay rule.
