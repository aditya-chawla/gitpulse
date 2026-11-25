# backend/cmd/gitpulse-mcp

Stdio-to-HTTP proxy for connecting local MCP clients (such as Claude Desktop) to a running `gitpulse` `/mcp` endpoint.

## Files

- `main.go` — Reads `GITPULSE_URL` and `GITPULSE_MCP_TOKEN` and forwards stdio JSON-RPC MCP requests over HTTP.
