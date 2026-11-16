-- Identity: GitHub user ID is the key everywhere (ready for multi-user).
CREATE TABLE users (
  id               BIGINT PRIMARY KEY,
  node_id          TEXT NOT NULL UNIQUE,
  login            TEXT NOT NULL,
  name             TEXT,
  avatar_url       TEXT,
  token_ciphertext BYTEA NOT NULL,
  token_nonce      BYTEA NOT NULL,
  token_scopes     TEXT NOT NULL DEFAULT '',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  token_hash  BYTEA PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE user_settings (
  user_id             BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  excluded_repo_ids   BIGINT[] NOT NULL DEFAULT '{}',
  excluded_repo_globs TEXT[]   NOT NULL DEFAULT '{}',
  include_forks       BOOLEAN  NOT NULL DEFAULT false,
  include_archived    BOOLEAN  NOT NULL DEFAULT false,
  timezone            TEXT     NOT NULL DEFAULT 'UTC'
);

CREATE TABLE scans (
  id             BIGSERIAL PRIMARY KEY,
  user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trigger        TEXT NOT NULL CHECK (trigger IN ('cron','ui')),
  status         TEXT NOT NULL CHECK (status IN ('running','succeeded','failed')),
  phase          TEXT,
  repos_total    INT NOT NULL DEFAULT 0,
  repos_done     INT NOT NULL DEFAULT 0,
  api_calls      INT NOT NULL DEFAULT 0,
  rate_remaining INT,
  error          TEXT,
  started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX scans_one_running ON scans (user_id) WHERE status = 'running';
CREATE INDEX scans_user_started_idx ON scans (user_id, started_at DESC);

CREATE TABLE repos (
  id                BIGINT PRIMARY KEY,
  user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  full_name         TEXT NOT NULL,
  first_seen        TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_scan_id BIGINT
);
CREATE INDEX repos_user_idx ON repos (user_id);

CREATE TABLE repo_snapshots (
  scan_id          BIGINT NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
  repo_id          BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  name             TEXT NOT NULL,
  description      TEXT,
  visibility       TEXT NOT NULL,
  is_fork          BOOLEAN NOT NULL,
  is_archived      BOOLEAN NOT NULL,
  stars            INT NOT NULL,
  forks            INT NOT NULL,
  size_kb          INT NOT NULL,
  default_branch   TEXT,
  topics           TEXT[] NOT NULL DEFAULT '{}',
  license_spdx     TEXT,
  created_at       TIMESTAMPTZ NOT NULL,
  pushed_at        TIMESTAMPTZ,
  has_readme       BOOLEAN NOT NULL,
  has_license      BOOLEAN NOT NULL,
  has_gitignore    BOOLEAN NOT NULL,
  has_ci           BOOLEAN NOT NULL,
  has_tests        BOOLEAN NOT NULL,
  open_prs         INT NOT NULL,
  open_issues      INT NOT NULL,
  health_score     SMALLINT NOT NULL,
  health_breakdown JSONB NOT NULL,
  status           TEXT NOT NULL CHECK (status IN ('active','slowing','dormant')),
  PRIMARY KEY (scan_id, repo_id)
);
CREATE INDEX repo_snapshots_repo_idx ON repo_snapshots (repo_id, scan_id DESC);

CREATE TABLE repo_languages (
  scan_id  BIGINT NOT NULL,
  repo_id  BIGINT NOT NULL,
  language TEXT NOT NULL,
  bytes    BIGINT NOT NULL,
  PRIMARY KEY (scan_id, repo_id, language),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE repo_commit_weeks (
  scan_id    BIGINT NOT NULL,
  repo_id    BIGINT NOT NULL,
  week_start DATE NOT NULL,
  total      INT NOT NULL,
  days       SMALLINT[] NOT NULL,
  PRIMARY KEY (scan_id, repo_id, week_start),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE repo_branches (
  scan_id        BIGINT NOT NULL,
  repo_id        BIGINT NOT NULL,
  name           TEXT NOT NULL,
  last_commit_at TIMESTAMPTZ,
  is_default     BOOLEAN NOT NULL,
  PRIMARY KEY (scan_id, repo_id, name),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE repo_items (
  scan_id                  BIGINT NOT NULL,
  repo_id                  BIGINT NOT NULL,
  kind                     TEXT NOT NULL CHECK (kind IN ('pr','issue')),
  number                   INT NOT NULL,
  title                    TEXT NOT NULL,
  author                   TEXT,
  url                      TEXT NOT NULL,
  is_draft                 BOOLEAN NOT NULL DEFAULT false,
  review_requested_from_me BOOLEAN NOT NULL DEFAULT false,
  assigned_to_me           BOOLEAN NOT NULL DEFAULT false,
  created_at               TIMESTAMPTZ NOT NULL,
  updated_at               TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (scan_id, repo_id, kind, number),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE repo_tech (
  scan_id  BIGINT NOT NULL,
  repo_id  BIGINT NOT NULL,
  tech     TEXT NOT NULL,
  category TEXT NOT NULL,
  source   TEXT NOT NULL,
  PRIMARY KEY (scan_id, repo_id, tech),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE workflow_runs (
  scan_id    BIGINT NOT NULL,
  repo_id    BIGINT NOT NULL,
  run_id     BIGINT NOT NULL,
  workflow   TEXT NOT NULL,
  branch     TEXT,
  conclusion TEXT NOT NULL,
  url        TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (scan_id, run_id),
  FOREIGN KEY (scan_id, repo_id) REFERENCES repo_snapshots (scan_id, repo_id) ON DELETE CASCADE
);

CREATE TABLE repo_stargazers (
  repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  kind    TEXT NOT NULL CHECK (kind IN ('star','fork')),
  actor   TEXT NOT NULL,
  at      TIMESTAMPTZ NOT NULL,
  first_seen_scan_id BIGINT NOT NULL,
  PRIMARY KEY (repo_id, kind, actor)
);

CREATE TABLE commits (
  repo_id     BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  sha         TEXT NOT NULL,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  authored_at TIMESTAMPTZ NOT NULL,
  additions   INT,
  deletions   INT,
  PRIMARY KEY (repo_id, sha)
);
CREATE INDEX commits_user_time_idx ON commits (user_id, authored_at);

CREATE TABLE http_cache (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  etag       TEXT NOT NULL,
  body       BYTEA NOT NULL,
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, url)
);
