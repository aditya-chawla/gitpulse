# backend/internal/llm

Optional server-side AI provider abstraction with monthly USD budget caps, response caching, and untrusted-input sanitization.

## Files

- `llm.go` — `Provider` interface, budget reservation/logging (`llm_calls`), and content-hash response cache (`llm_cache`).
- `anthropic.go` — Anthropic Messages API client implementation.
- `pricing.go` — Built-in token pricing table and cost estimation.
- `sanitize.go` — Prompt-injection boundary helpers (`WrapUntrusted`) for external PR/repo text.
- `anthropic_test.go` / `llm_test.go` / `sanitize_test.go` — Unit tests for Anthropic client, budget caps, caching, and sanitization.
