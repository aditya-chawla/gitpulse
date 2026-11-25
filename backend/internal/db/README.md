# backend/internal/db

PostgreSQL connection management and embedded SQL schema migrations.

## Files

- `db.go` — `pgxpool` connection setup and `golang-migrate` runner embedding `migrations/*.sql`.
- `db_test.go` — Integration test verifying up/down migrations against `TEST_DATABASE_URL`.

## Subfolders

- `migrations/` — Sequential `.up.sql` and `.down.sql` schema migrations (`0001` through `0009`).
