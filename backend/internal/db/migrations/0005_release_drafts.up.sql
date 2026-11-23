-- Phase 2, step 4: audit trail of draft releases gitpulse created.
CREATE TABLE release_drafts (
  id                BIGSERIAL PRIMARY KEY,
  user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_id           BIGINT NOT NULL,
  repo_name         TEXT NOT NULL,
  tag               TEXT NOT NULL,
  base_tag          TEXT,
  github_release_id BIGINT NOT NULL,
  html_url          TEXT NOT NULL,
  source            TEXT NOT NULL CHECK (source IN ('ui','mcp')),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX release_drafts_user_idx ON release_drafts (user_id, created_at DESC);
