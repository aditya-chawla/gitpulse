-- Revert Milestone 0, step 2: Trend snapshots.

DROP INDEX IF EXISTS repo_trend_snapshots_lookup_idx;
DROP TABLE IF EXISTS repo_trend_snapshots;
