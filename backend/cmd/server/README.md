# backend/cmd/server

Main `gitpulse` backend server entrypoint.

## Files

- `main.go` — Loads configuration, runs PostgreSQL migrations, connects to the database pool, initializes all services (`github`, `install`, `scanner`, `watchlist`, `stats`, `advisor`, `prreview`, `releases`, `profile`, `mcpserver`), and runs the HTTP server with graceful shutdown.
