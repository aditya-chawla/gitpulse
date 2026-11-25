# backend/internal/auth

Authentication and session management via GitHub App OAuth.

## Files

- `auth.go` — OAuth login/callback/logout handlers, `ALLOWED_USERS` allowlist enforcement, `RequireUser` middleware, and `/api/me`.
- `tokens.go` — Cryptographically random session token generation and SHA-256 cookie hashing.
- `oauth_test.go` — OAuth callback, allowlist rejection, and session cookie tests.
