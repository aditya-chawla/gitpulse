# backend/internal/db/migrations

Ordered PostgreSQL 17 schema migrations.

## Files

- `0001_init.{up,down}.sql` — Core tables: `users`, `sessions`, `user_settings`, `scans`, `repos`, `repo_snapshots`, `repo_languages`, `repo_commit_weeks`, `repo_branches`, `repo_items`, `repo_tech`, `workflow_runs`, `repo_stargazers`, `commits`, `http_cache`.
- `0002_github_app.{up,down}.sql` — `github_installations` and `repo_settings` (per-repo PR review and release toggles).
- `0003_llm.{up,down}.sql` — `llm_calls` (budget/cost ledger) and `llm_cache`.
- `0004_pr_reviews.{up,down}.sql` — `pr_reviews` queue/results and `pr_comments` tracking.
- `0005_release_drafts.{up,down}.sql` — `release_drafts` history.
- `0006_profile_readme.{up,down}.sql` — `profile_readme_runs` audit log.
- `0007_watchlist.{up,down}.sql` — `watched_repos` table and `repos.source` (`owned` vs `watched`) column.
- `0008_trends.{up,down}.sql` — `repo_trend_snapshots` denormalized time-series table for 7d/30d deltas and Postgres DS queries.
- `0009_action_queue.{up,down}.sql` — `action_items` table with persistent dismissal per `(user_id, repo_id, rule)`.
