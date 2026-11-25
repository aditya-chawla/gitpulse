-- Revert Milestone 0, step 1: Watchlist.

DROP INDEX IF EXISTS repos_source_idx;

ALTER TABLE repos
  DROP COLUMN IF EXISTS watched_repo_id,
  DROP COLUMN IF EXISTS source;

DROP TABLE IF EXISTS watched_repos;
