-- Milestone 0, step 3: Action queue — prioritised "what to fix next" items.
-- Keyed by (user_id, repo_id, rule) so dismissed state persists across scans.

CREATE TABLE action_items (
  id           BIGSERIAL PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_id      BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  rule         TEXT NOT NULL,                        -- e.g. "pr_bottleneck", "stale_branches", "no_ci"
  severity     SMALLINT NOT NULL,                    -- 1 (info) → 5 (critical)
  title        TEXT NOT NULL,                        -- plain-English one-liner
  detail       JSONB NOT NULL DEFAULT '{}',          -- rule-specific payload (PR numbers, branch names, etc.)
  url          TEXT,                                 -- deep link to the offending resource
  active       BOOLEAN NOT NULL DEFAULT true,        -- false when the rule no longer triggers
  dismissed    BOOLEAN NOT NULL DEFAULT false,       -- user-dismissed; persists across scans
  last_scan_id BIGINT NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, repo_id, rule)
);

-- Dashboard query: active, non-dismissed items for a user, ordered by severity.
CREATE INDEX action_items_active_idx
  ON action_items (user_id, severity DESC, updated_at DESC)
  WHERE active AND NOT dismissed;
