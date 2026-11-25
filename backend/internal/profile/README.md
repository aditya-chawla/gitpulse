# backend/internal/profile

Automated GitHub Profile README updater (`<login>/<login>`).

## Files

- `readme.go` — Replaces `<!-- gitpulse:start -->` / `<!-- gitpulse:end -->` blocks with language stats, active projects, and recent Medium posts.
- `medium.go` — Fetches and parses a user's Medium RSS feed.
- `service.go` — Previews and commits profile README updates (only when content changed) using a single-repo `contents:write` token.
- `readme_test.go` / `service_test.go` — Unit tests for marker replacement and commit-if-changed logic.
