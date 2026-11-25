# backend/internal/detect

Deterministic repository inspection for documentation, CI/CD, tests, and tech stack detection.

## Files

- `detect.go` — Inspects file trees and manifests (`go.mod`, `package.json`, `pyproject.toml`, `.github/workflows`, etc.) up to 4 directory levels deep to detect README, license, `.gitignore`, CI, tests, and technologies.
- `detect_test.go` — Unit tests for manifest parsing and CI/test detection rules.
