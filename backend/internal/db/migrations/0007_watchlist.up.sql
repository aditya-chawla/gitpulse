-- Milestone 0, step 1: Watchlist — track any public repository.

CREATE TABLE watched_repos (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  owner      TEXT NOT NULL,
  name       TEXT NOT NULL,
  github_id  BIGINT,              -- filled on first successful scan
  added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  UNIQUE (user_id, owner, name)
);

-- Extend repos so the same snapshot pipeline stores both owned and watched repos.
ALTER TABLE repos
  ADD COLUMN source TEXT NOT NULL DEFAULT 'owned'
    CHECK (source IN ('owned', 'watched')),
  ADD COLUMN watched_repo_id BIGINT REFERENCES watched_repos(id) ON DELETE SET NULL;

CREATE INDEX repos_source_idx ON repos (user_id, source);
