-- Phase 2, step 2: LLM usage log (budget source of truth) and output cache.
CREATE TABLE llm_calls (
  id                 BIGSERIAL PRIMARY KEY,
  user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feature            TEXT NOT NULL,                 -- pr_review | release_notes
  repo_id            BIGINT,                        -- no FK: keep the cost history if a repo is deleted
  provider           TEXT NOT NULL,
  model              TEXT NOT NULL,
  status             TEXT NOT NULL CHECK (status IN
                       ('reserved','ok','cache_hit','refused','error','over_budget','too_large')),
  input_tokens       INT NOT NULL DEFAULT 0,
  output_tokens      INT NOT NULL DEFAULT 0,
  estimated_cost_usd NUMERIC(12,6) NOT NULL DEFAULT 0,  -- worst case reserved before the call
  cost_usd           NUMERIC(12,6) NOT NULL DEFAULT 0,  -- actual, from reported usage
  cache_key          BYTEA,
  error              TEXT,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at        TIMESTAMPTZ
);
CREATE INDEX llm_calls_user_month_idx ON llm_calls (user_id, created_at);

CREATE TABLE llm_cache (
  cache_key    BYTEA PRIMARY KEY,  -- sha256(provider, model, prompt version, system, input, schema)
  feature      TEXT NOT NULL,
  output       JSONB NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
