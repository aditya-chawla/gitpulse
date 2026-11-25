# backend/internal/releases

Release notes generator and GitHub draft release creator.

## Files

- `notes.go` — Groups merged PRs since the base tag into features, fixes, dependencies, and internal changes, and suggests the next semver tag.
- `service.go` — Previews release notes and creates GitHub *draft* releases (explicitly blocked for read-only `watched` repositories).
- `notes_test.go` / `service_test.go` — Unit tests for PR grouping, semver bumping, and watched-repo guards.
