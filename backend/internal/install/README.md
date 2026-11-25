# backend/internal/install

GitHub App installation discovery and least-privilege token minting.

## Files

- `install.go` — Resolves a user's GitHub App installation ID, checks granted permissions (e.g., `actions:read`), and mints read-only or single-repo scoped write tokens.
