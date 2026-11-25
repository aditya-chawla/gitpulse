# backend/internal/insights

Deterministic Data Science metrics computed from Postgres snapshot aggregates.

## Files

- `insights.go` — Computes Z-score anomalies (`>= 1.5σ`), 30-day linear health projections, PR responsiveness (`none`/`fast`/`moderate`/`slow`), contributor diversity (`0.0–1.0`), and `contributorReadiness` (`0–100` + verdict).
- `insights_test.go` — Unit tests for anomaly detection, diversity scoring, and readiness verdicts.
