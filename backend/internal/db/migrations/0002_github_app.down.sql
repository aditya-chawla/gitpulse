DROP TABLE IF EXISTS repo_settings, github_installations;
-- Tokens cannot be restored; users must sign in again after a rollback.
ALTER TABLE users
  ADD COLUMN token_ciphertext BYTEA NOT NULL DEFAULT '\x',
  ADD COLUMN token_nonce      BYTEA NOT NULL DEFAULT '\x',
  ADD COLUMN token_scopes     TEXT  NOT NULL DEFAULT '';
