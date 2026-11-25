-- Milestone 0, step 2: Trend snapshots — denormalised summary per repo per scan
-- for fast 7-day / 30-day delta queries.

CREATE TABLE repo_trend_snapshots (
  id                 BIGSERIAL PRIMARY KEY,
  repo_id            BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  scan_id            BIGINT NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
  scanned_at         TIMESTAMPTZ NOT NULL,          -- scans.started_at, copied here for fast lookups
  health_score       SMALLINT NOT NULL,
  open_prs           INT NOT NULL,
  open_issues        INT NOT NULL,
  stale_branches     INT NOT NULL,
  failed_runs        INT NOT NULL,
  commit_count_7d    INT NOT NULL,                  -- commits in the 7 days ending at scan time
  commit_count_30d   INT NOT NULL,
  unique_authors_30d INT NOT NULL,                  -- distinct commit authors in last 30 days
  top_author_login   TEXT,                           -- author with most commits in last 30 days
  top_author_pct     SMALLINT NOT NULL DEFAULT 0,    -- that author's share of 30-day commits (0–100)
  stars              INT NOT NULL,
  forks              INT NOT NULL,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, scan_id)
);

-- Used to find the closest snapshot ≥ 7 days ago and ≥ 30 days ago.
CREATE INDEX repo_trend_snapshots_lookup_idx
  ON repo_trend_snapshots (repo_id, scanned_at DESC);
