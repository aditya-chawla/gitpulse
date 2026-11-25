# backend/internal/watchlist

Public repository watchlist management for `OSS Explorer`.

## Files

- `watchlist.go` — Validates `owner/repo` slugs or GitHub URLs, verifies public repository existence via GitHub API, and manages `watched_repos` rows.
- `watchlist_test.go` — Unit tests for URL/slug parsing and watchlist CRUD.
