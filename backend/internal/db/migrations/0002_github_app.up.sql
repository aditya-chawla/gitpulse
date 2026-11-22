-- Phase 2, step 1: GitHub App. User tokens are no longer stored; scans and
-- writes use short-lived installation tokens minted from the app's key.
ALTER TABLE users
  DROP COLUMN token_ciphertext,
  DROP COLUMN token_nonce,
  DROP COLUMN token_scopes;

CREATE TABLE github_installations (
  id                   BIGINT PRIMARY KEY,                -- installation id
  account_id           BIGINT NOT NULL UNIQUE,            -- = users.id for user installs
  account_login        TEXT NOT NULL,
  repository_selection TEXT NOT NULL CHECK (repository_selection IN ('all','selected')),
  html_url             TEXT,
  permissions          JSONB NOT NULL DEFAULT '{}',       -- accepted permissions, e.g. {"contents":"write"}
  suspended_at         TIMESTAMPTZ,
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-repo automation switches. A missing row means the defaults below:
-- PR assistant shows reviews in the dashboard only; nothing is ever written.
CREATE TABLE repo_settings (
  user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repo_id          BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  pr_mode          TEXT NOT NULL DEFAULT 'dashboard' CHECK (pr_mode IN ('off','dashboard','comment')),
  releases_enabled BOOLEAN NOT NULL DEFAULT false,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, repo_id)
);
