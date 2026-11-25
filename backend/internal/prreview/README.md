# backend/internal/prreview

Automated Pull Request Assistant for owned repositories.

## Files

- `analysis.go` — Deterministic diff/path rules (risky changes such as migrations, auth, CI/deploy configs, lockfiles, large files) and conventional-commit/path label suggestions.
- `ai.go` — Optional LLM PR summary generator with deterministic fallback.
- `comment.go` — Formats and upserts one self-updating `gitpulse` PR comment.
- `service.go` — Background worker queue, webhook ingestion, and 30-minute catch-up sync (skipping watched repos).
- `analysis_test.go` / `service_test.go` — Unit tests for flag/label rules and idempotent review queuing.
