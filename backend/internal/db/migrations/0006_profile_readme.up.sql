-- Phase 2, step 5: profile README updater settings and run log.
ALTER TABLE user_settings
  ADD COLUMN medium_feed_url        TEXT,
  ADD COLUMN profile_readme_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE profile_readme_runs (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_name  TEXT NOT NULL,
  trigger    TEXT NOT NULL CHECK (trigger IN ('ui','cron')),
  outcome    TEXT NOT NULL CHECK (outcome IN ('updated','unchanged','no_markers','disabled','failed')),
  commit_sha TEXT,
  commit_url TEXT,
  error      TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX profile_readme_runs_user_idx ON profile_readme_runs (user_id, created_at DESC);
