# backend/internal/mcpserver

Model Context Protocol (MCP) server exposed over streamable HTTP (`/mcp`).

## Files

- `server.go` — Registers read-only tools (`list_repos_needing_attention`, `get_repo_health`, `get_language_stats`, `get_activity`, `get_digest`, `get_pr_reviews`, `get_trends`, `get_action_items`, `get_repo_insights`, `compare_watched_repos`) and optional write tools (`draft_release_notes`, `dismiss_action`), plus bearer-token HTTP handler.
- `live.go` — Adapts the live `store`, `stats`, `prreview`, and `releases` services to the MCP `Backend` interface.
- `server_test.go` — Tests tool registration, bearer auth, watched-repo write guards, and tool outputs.
