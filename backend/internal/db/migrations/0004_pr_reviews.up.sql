-- Phase 2, step 3: PR assistant. One row per (repo, PR, head SHA): the
-- unique key makes webhook deliveries and catch-up syncs idempotent.
CREATE TABLE pr_reviews (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_id     BIGINT NOT NULL,          -- no FK: a PR can arrive before the repo's first scan
  repo_name   TEXT NOT NULL,            -- owner/name at enqueue time
  pr_number   INT NOT NULL,
  head_sha    TEXT NOT NULL,
  title       TEXT,
  author      TEXT,
  url         TEXT,
  source      TEXT NOT NULL CHECK (source IN ('webhook','sync','manual')),
  status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','running','done','skipped','failed')),
  mode        TEXT,                     -- pr_mode used when it ran
  result      JSONB,                    -- analysis + optional AI summary
  ai_status   TEXT,                     -- ok | cached | disabled | over_budget | too_large | refused | error
  skip_reason TEXT,
  error       TEXT,
  attempts    INT NOT NULL DEFAULT 0,
  comment_url TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (repo_id, pr_number, head_sha)
);
CREATE INDEX pr_reviews_queue_idx ON pr_reviews (status, created_at) WHERE status IN ('pending','running');
CREATE INDEX pr_reviews_user_idx ON pr_reviews (user_id, created_at DESC);

-- The single gitpulse comment per PR, edited on every new push.
CREATE TABLE pr_comments (
  repo_id    BIGINT NOT NULL,
  pr_number  INT NOT NULL,
  comment_id BIGINT NOT NULL,
  html_url   TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (repo_id, pr_number)
);
