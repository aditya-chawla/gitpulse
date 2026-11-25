# backend/internal/config

Runtime configuration loading and validation from environment variables.

## Files

- `config.go` — Parses and validates `PORT`, `DATABASE_URL`, GitHub App credentials, `ALLOWED_USERS`, `SCAN_SECRET`, `GITHUB_PUBLIC_TOKEN`, `MCP_*`, cookie/CORS flags, and optional `LLM_*` settings.
- `config_test.go` — Unit tests for required/optional config validation and LLM pricing defaults.
