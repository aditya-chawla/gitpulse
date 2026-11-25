# backend/internal/advisor

Context-aware **Ask GitPulse** assistant supporting both **Maintainer Copilot** (`My Repos`) and **Contributor Advisor** (`OSS Explorer`) personas.

## Files

- `advisor.go` — Request/response types (`AskRequest`, `AskResponse`), role normalization, Data Science + signal context loading, and orchestration.
- `deterministic.go` — Zero-cost rule-based advisor that synthesizes Postgres Data Science metrics, Z-score anomalies, and action items into plain-English guidance.
- `byok.go` — Bring-Your-Own-Key HTTP clients for Google Gemini (`gemini-3.8-flash` / `gemini-2.5-flash`), OpenAI (`gpt-4o-mini`), and Anthropic (`claude-haiku-4-5-20251001`) with automatic deterministic fallback on failure.
- `advisor_test.go` — Unit tests for both deterministic roles and mocked BYOK provider calls/fallbacks.
