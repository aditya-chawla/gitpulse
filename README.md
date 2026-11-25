# gitpulse

A **Repository Intelligence, Maintenance & Open-Source Explorer Dashboard** built on a snapshot-first PostgreSQL 17 architecture.

### What It Does

1. **Two-Mode Workspace (`My Repos` | `OSS Explorer`):**
   - **`My Repos` (Maintainer Mode):**
     - **Analytics:** Language mix (overall & per repo), language trends by year, weekly commits, commit heatmap (day × hour in your timezone), streaks, top repos, and detected tech stack.
     - **Repo health & 7d/30d trends:** 0–100 score per repo with visible breakdown, `active`/`slowing`/`dormant` status, and `▲ Improving` / `— Steady` / `▼ Slipping` trend badges.
     - **What to Fix Next:** Severity-weighted maintenance queue (`ci_rot`, `pr_bottleneck`, `stale_branches`, `commit_cadence_drop`, `bus_factor`, `unprotected_code`, `no_recent_release`) with persistent dismissals.
     - **Digest:** PRs waiting on you, new issues, failed workflow runs, and star/fork deltas since the previous scan.
     - **PR assistant:** Automated commit-by-commit PR reviews (risky file flags, label suggestions, optional AI summary).
     - **Release notes & Profile README:** Semver-suggested *draft* releases and automated profile README updates.
   - **`OSS Explorer` (Contributor / Watchlist Mode):**
     - Track any public `owner/repo` without installing the GitHub App on that repository.
     - Evaluate commit cadence (7d/30d), contributor spread (bus factor), PR responsiveness, and read-only project signals before or while contributing.
2. **Postgres 17 Data Science Engine (`$0` External Compute):**
   - Computes **Z-score anomalies** (`AVG` / `STDDEV_POP`), **PR age percentiles** (`PERCENTILE_CONT`), **30-day health trajectory regression slopes** (`REGR_SLOPE`), **contributor diversity scores**, and **contributor readiness** verdicts natively in SQL.
3. **Context-Aware `Ask AI ✦` Side Panel (BYOK + Free Deterministic):**
   - Adapts its persona between **Maintainer Copilot** (`My Repos`) and **Contributor Advisor** (`OSS Explorer`).
   - Works out-of-the-box in **Free Deterministic Mode** ($0 API spend) and supports **Bring-Your-Own-Key (BYOK)** for **Google Gemini**, **OpenAI GPT**, and **Anthropic Claude** (keys stay in browser `localStorage`).
4. **1-Click Read-Only Live Demo (`/?demo=1`):**
   - Visitors can click **View live demo (read-only)** on `/login` to explore both modes, Data Science signals, and the deterministic advisor with zero login or database writes.
5. **MCP Server (`/mcp` & `cmd/gitpulse-mcp`):**
   - Exposes repo health, trends, action items, Data Science insights (`get_repo_insights`), and watchlist comparisons (`compare_watched_repos`) to Claude Desktop, Cursor, or Gemini CLI.

### How It Works

Every dashboard page reads from **PostgreSQL snapshots**, never live GitHub API calls on page load. A **scan** (triggered daily via GitHub Actions or via **Scan now**) fetches repository metadata, languages, commits, open PRs/issues, branches, and workflow runs using GraphQL + ETag-cached REST requests, computes health scores, trends, and decay alerts, and persists them as time-series snapshots.

> Each folder in this repository contains its own `README.md` documenting every file inside it.

```text
backend/                 Go 1.27 · chi · pgx · embedded SQL migrations
  cmd/server             HTTP server entrypoint
  cmd/gitpulse-mcp       stdio proxy for local MCP clients
  internal/advisor       Role-aware AI advisor (deterministic + BYOK Gemini/OpenAI/Claude)
  internal/api           REST handlers, routing, CORS, CSRF origin checks
  internal/auth          GitHub App OAuth sign-in, allowlist, cookie sessions
  internal/config        Environment configuration & validation
  internal/db            Postgres connection pool + embedded SQL migrations
  internal/detect        README, CI, test, and tech-stack detection
  internal/github        GitHub client (GraphQL/REST, ETag cache, JWT app auth)
  internal/health        0–100 health scoring & activity status
  internal/insights      Statistical & Data Science metrics (Z-scores, diversity, readiness)
  internal/install       GitHub App installation lookup & scoped tokens
  internal/llm           Optional server-side AI provider, budget cap, cache
  internal/mcpserver     MCP tools over streamable HTTP (/mcp)
  internal/profile       Profile README updater & Medium RSS sync
  internal/prreview      PR assistant webhook/sync queue & comment upsert
  internal/releases      Release note grouping, semver & draft creation
  internal/scanner       Background scan orchestrator & snapshot persistence
  internal/stats         Dashboard queries, Postgres DS SQL, trends & action items
  internal/store         User, session & per-repo automation settings store
  internal/trends        7d/30d delta evaluation & silent decay rules
  internal/watchlist     Public repository watchlist CRUD & lookup
frontend/                React 19 · TypeScript · Vite · Tailwind 4 · Recharts
.github/workflows/       ci.yml · scan.yml · pr-sync.yml · profile-readme.yml
render.yaml              Render Blueprint (API + static site)
Dockerfile.combined      Single-service Docker build (Go API + built frontend)
docker-compose.yml       Local Postgres 17 + backend + frontend
```

---

## Local setup

**Prerequisites:** Docker Desktop. Go 1.27+ and Node 24+ are only needed to run things outside Docker.

1. **Create a GitHub App for local use and install it** (see [GitHub App](#github-app)), with callback URL
   `http://localhost:8080/api/auth/callback`.
2. **Configure the backend:**
   ```sh
   cp backend/.env.example backend/.env
   ```
   Fill in `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_APP_PRIVATE_KEY` and `SCAN_SECRET`. The other defaults already suit local use. Generate the scan secret with:
   ```sh
   openssl rand -hex 32      # SCAN_SECRET
   ```
   (PowerShell: `-join ((1..32)|%{'{0:x2}' -f (Get-Random -Max 256)})`.)
3. **Start everything:**
   ```sh
   docker compose up --build
   ```
   - Frontend: <http://localhost:5173> (Vite dev server; `/api` is proxied to the backend)
   - Backend: <http://localhost:8080> (`/healthz`, `/readyz`)
   - Postgres: `localhost:5433` (user/password/db `gitpulse`)
4. Open <http://localhost:5173>, click **Sign in with GitHub**, then **Scan now**. A first scan of about 50 repos takes 1–2 minutes and uses fewer than 100 API calls.

### Without Docker (faster iteration)

```sh
docker compose up -d db                 # just Postgres
cd backend && go run ./cmd/server       # reads env vars; e.g. load backend/.env with direnv or your IDE
cd frontend && npm install && npm run dev
```

### Tests and checks

```sh
cd backend
go vet ./...
go test ./...                           # unit tests
# Scanner integration tests need a throwaway database:
docker compose exec db psql -U gitpulse -c 'CREATE DATABASE gitpulse_test'
TEST_DATABASE_URL=postgres://gitpulse:gitpulse@localhost:5433/gitpulse_test?sslmode=disable go test ./...

cd frontend
npm run lint && npm run typecheck && npm run build
```

---

## GitHub App

gitpulse is a **GitHub App**. One app does two jobs:

- **Sign-in.** "Sign in with GitHub" uses the app's user authorization. gitpulse uses the user token once to learn who you are, checks `ALLOWED_USERS`, then revokes the token. No user token is stored.
- **API access.** Scans and write features use short-lived (1 hour) **installation tokens** minted with the app's private key. Scans ask for read-only tokens. Each write asks for a token limited to **one repository** and the single permission it needs.

Each app has one webhook URL, so create **two apps**: one for local development, one for production.

### Create the app

Go to <https://github.com/settings/apps/new> and fill in:

| Field | Local app | Production app |
|---|---|---|
| GitHub App name | `gitpulse-dev-<you>` (must be unique on GitHub) | `gitpulse-<you>` |
| Homepage URL | `http://localhost:5173` | `https://<static-site-host>` |
| Callback URL | `http://localhost:8080/api/auth/callback` | `https://<static-site-host>/api/auth/callback` |
| Expire user authorization tokens | on (default) | on (default) |
| Request user authorization (OAuth) during installation | off | off |
| Webhook → Active | see [webhook setup (local)](#webhook-setup-local-via-smeeio) | see [webhook setup (production)](#webhook-setup-production) |
| Where can this GitHub App be installed? | Only on this account | Only on this account |

The production callback goes through the **frontend** host. Render rewrites `/api/*` to the backend, so the session cookie belongs to the site you browse.

**Repository permissions.** Grant only these; leave everything else, including all account permissions, at "No access":

| Permission | Access | Why gitpulse needs it |
|---|---|---|
| Metadata | Read | Mandatory for every app. Lists your repos, branches, languages, topics, stars and forks. |
| Contents | Read & write | **Read:** file trees and manifests (README/CI/test/stack detection), commit history and stats, tags. **Write:** only for two features, both off by default: creating *draft* releases and committing your profile README. gitpulse never pushes code. |
| Pull requests | Read & write | **Read:** open PRs for health and the digest, and PR diffs for the PR assistant. **Write:** only to create or edit gitpulse's single comment on a PR, in repos where you chose "Comment on PR". Titles and labels are never changed. |
| Issues | Read | Open issues for the digest, and each repo's existing labels (the PR assistant only suggests labels that already exist). |
| Actions | Read | Failed workflow runs for the digest. |

Then:

1. **Create GitHub App.** On the app page, copy the **Client ID**, **Generate a new client secret**, and under **Private keys**, **Generate a private key** (a `.pem` downloads).
2. Put them in `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` and `GITHUB_APP_PRIVATE_KEY`: in `backend/.env` locally, in the Render dashboard for production. The key goes in as base64 on one line:
   ```sh
   base64 -w0 gitpulse.private-key.pem     # macOS: base64 -i gitpulse.private-key.pem
   ```
   (PowerShell: `[Convert]::ToBase64String([IO.File]::ReadAllBytes("gitpulse.private-key.pem"))`.) Then delete the downloaded file, or keep it in a password manager.
3. **Install App** → your account → **All repositories**.

### Why "All repositories"

A GitHub App sees only the private repos it is installed on. Installing on all repos keeps analytics and health complete. GitHub grants permissions per installation, not per repo, so gitpulse enforces writes itself:

- Every write feature has a per-repo switch in **Settings → Repository automation**, and all are **off** by default.
- The PR assistant defaults to **Dashboard only**: it summarises PRs inside gitpulse and posts nothing.
- A write uses a token minted for that one repo, so even a bug can't touch a different repo.

If you install on selected repos instead, unselected private repos drop out of analytics and health.

### Migrating from the v1 OAuth app

The database migration drops the stored OAuth tokens. Scans and history stay; nothing else changes.

1. Create the production GitHub App as above, and install it on all repositories.
2. In Render → `gitpulse-api` → **Environment**:
   - Replace `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` with the **app's** values.
   - Add `GITHUB_APP_PRIVATE_KEY` (base64 of the `.pem`).
   - Delete `TOKEN_ENC_KEY`; it's no longer used.
   - Keep `OAUTH_REDIRECT_URL`, and make sure the app's callback URL is exactly the same.
3. Deploy. Your existing session keeps working. Run **Scan now** to confirm, or sign out and back in through the app.
4. Delete the old OAuth app (github.com/settings/developers), and revoke its access under github.com/settings/applications → **Authorized OAuth Apps**.

---

## Neon database (free)

1. Sign up at <https://neon.tech> and create a project. Postgres 17 is fine. Pick the region closest to your Render region; the blueprint uses Render's **Singapore** region, so choose **AWS Asia Pacific (Singapore)**.
2. On the project dashboard, open **Connect**, select the default database and role, and **turn off "Connection pooling"**. Use the *direct* connection string: migrations use advisory locks, which the pooler doesn't support.
3. Copy the string. It looks like
   `postgresql://user:password@ep-xxxx.ap-southeast-1.aws.neon.tech/neondb?sslmode=require`.
   This is your production `DATABASE_URL`. Tables are created automatically on the backend's first start.

The free tier's 0.5 GB is plenty. Detail rows (branches, PRs, weekly commits…) are pruned after 30 scans, while compact per-repo snapshots are kept for trend charts.

---

## Deploy to Render (free)

`render.yaml` defines two free services:

- **`gitpulse-api`**: Docker web service built from `backend/Dockerfile` (distroless, ~24 MB), health check `/healthz`. It deploys after CI passes on `main`.
- **`gitpulse`**: static site built from `frontend/`. It **rewrites `/api/*` to the API**, so the browser talks to one origin.

### Steps

1. Push this repo to GitHub (done if you're reading it there).
2. In Render: **New → Blueprint**, connect the `gitpulse` repository, and Render reads `render.yaml`.
3. Render asks for the `sync: false` values:
   - `DATABASE_URL`: the Neon direct connection string.
   - `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_APP_PRIVATE_KEY`: from the **production** GitHub App. If you haven't made it yet, enter placeholders and fix them in step 6.
   - `OAUTH_REDIRECT_URL`: `https://<static-site-host>/api/auth/callback`.
   - `CORS_ALLOWED_ORIGINS`: `https://<static-site-host>`.

   Service hostnames are global, so if `gitpulse` or `gitpulse-api` is taken, Render adds a suffix. You'll see the real hosts after creation; placeholders are fine for now.
   `SCAN_SECRET` is generated by Render.
4. Click **Apply**. When the services exist, note both URLs (e.g. `https://gitpulse-xxxx.onrender.com` and `https://gitpulse-api-xxxx.onrender.com`).
5. **If the API host isn't exactly `gitpulse-api.onrender.com`**, edit the rewrite `destination` in `render.yaml` to the real host and push. The static site redeploys.
6. Fix the values that depend on hosts: in the API service's **Environment** tab, set `OAUTH_REDIRECT_URL` and `CORS_ALLOWED_ORIGINS` to the real static-site host, and update the production GitHub App's Homepage and Callback URLs to match.
7. Open the static site, sign in, and press **Scan now**.

### Verify the rewrite proxies correctly

Render's docs don't spell out whether a rewrite to an external URL forwards POST bodies and `Set-Cookie`. Check once after deploying:

```sh
SITE=https://<static-site-host>
curl -si $SITE/api/auth/login | grep -iE '^(HTTP|location|set-cookie)'
#  expect: 302, a github.com/login/oauth/authorize Location, and a Set-Cookie for gitpulse_oauth_state
curl -si -X POST $SITE/api/scan | head -1
#  expect: HTTP 401 (reached the API; not signed in). A 404/405 means POSTs aren't forwarded.
```

Then sign in through the browser. If you end up back on the login page, cookies aren't surviving the rewrite. Switch to the fallback below.

### Single-service fallback

If the rewrite doesn't proxy correctly, serve the frontend from the Go server instead:

1. In Render, delete the static site. Then edit the API service: **Settings → Build & Deploy**, set Dockerfile path `./Dockerfile.combined` and Docker context `.` (repo root). Also remove the `buildFilter`, or add `frontend/**` to it, so frontend changes redeploy.
   Or edit `render.yaml` to match and re-sync the Blueprint.
2. The image sets `STATIC_DIR=/web` itself. Don't define `STATIC_DIR` in the service's environment, because an empty value overrides it.
3. Use the API's own host everywhere: the app's callback `https://<api-host>/api/auth/callback`, `CORS_ALLOWED_ORIGINS` empty.

### Cross-origin setup (not recommended)

To keep two separate hosts without a rewrite: build the frontend with `VITE_API_BASE_URL=https://<api-host>`, and set `CORS_ALLOWED_ORIGINS=https://<static-site-host>`, `COOKIE_SAMESITE=none` and `COOKIE_SECURE=true` on the API. Browsers that block third-party cookies (Safari, and others by setting) will then fail to keep you signed in, which is why same-origin is the default.

---

## Daily scan (GitHub Actions)

Free Render services sleep after 15 idle minutes, so the app has no in-process scheduler. `.github/workflows/scan.yml` runs daily at 02:17 UTC (and on demand via **Actions → Daily scan → Run workflow**). It:

1. retries `/healthz` for up to ~15 minutes to survive a cold start,
2. calls `POST /api/scan` with the `X-Scan-Secret` header (scans every allowlisted user who has signed in once and installed the app),
3. polls `GET /api/scans/{id}` every 30 s until the scan finishes. This keeps the instance awake and fails the workflow if the scan fails.

**Repository secrets** (GitHub → repo → Settings → Secrets and variables → Actions → New repository secret):

| Secret | Value |
|---|---|
| `GITPULSE_API_URL` | The API's public URL, e.g. `https://gitpulse-api.onrender.com` (no trailing slash). In the single-service fallback, the one service's URL. |
| `SCAN_SECRET` | Copy the `SCAN_SECRET` value from the Render API service's Environment tab. |

`pr-sync.yml` (the [PR assistant](#pr-assistant)'s 30-minute catch-up) and `profile-readme.yml` (the weekly [profile README](#profile-readme) refresh) use the same two secrets. CI (`ci.yml`) needs no secrets.

---

## PR assistant

Every new head commit of an open pull request in your repos is reviewed **once**. The idempotency key is repo + PR number + head SHA. Results show on the **Pull requests** page.

**What a review contains** (no API key needed):

- files changed, additions and deletions;
- **notable changes** found by rules: database migrations, auth/session/security code, deployment/CI/config files, dependency manifests and lock files, deleted tests, files with over 500 changed lines, and binary files;
- **suggested labels**, chosen only from labels that already exist in the repo. The rules use the PR title's conventional-commit prefix (`feat:` → enhancement, `fix:` → bug, …), commit prefixes when most commits agree, dependency bots, and paths (docs-only, tests-only, workflows-only, manifests-only, migrations);
- **what was left out:**
  - lock files, vendored code and generated/minified files;
  - binary files and diffs GitHub won't show;
  - files cut at 6,000 characters;
  - files over the total size limit.

With an API key, the review also gets an AI summary, more notable points and a suggested title. The AI's label picks are filtered the same way. See [AI (optional)](#ai-optional).

**Per-repo modes** (Settings → Repository automation):

| Mode | Behaviour |
|---|---|
| Off | Never reviewed. |
| **Dashboard only** (default) | Reviewed and shown in gitpulse. Nothing is posted to GitHub. |
| Comment on PR | Also keeps **one** comment on the PR, marked with `<!-- gitpulse:pr-review -->`, and edits it on each push instead of posting again. |

gitpulse never changes PR titles or labels; it only suggests them. Comments are written with an installation token minted for that one repo, and that token has only the `pull_requests: write` permission.

**How PRs arrive:**

1. **Webhook** (`POST /api/webhooks/github`). GitHub sends `pull_request` events (opened, synchronize, reopened, ready_for_review):
   - Each delivery is checked against `GITHUB_WEBHOOK_SECRET` (HMAC-SHA256, constant-time) before parsing.
   - The request is queued in Postgres and answered `202` immediately.
   - A background worker does the review.
2. **Catch-up** (`POST /api/prs/sync`, `X-Scan-Secret` or a session). It queues every open PR whose head commit hasn't been reviewed yet.
   - `.github/workflows/pr-sync.yml` calls it every 30 minutes and retries through cold starts. The free Render instance sleeps, and GitHub gives up on a webhook after 10 seconds, so deliveries can be missed.
   - The **Check for new PRs** button on the Pull requests page does the same.

Both paths are idempotent, so overlapping deliveries and syncs are harmless. Failed reviews are retried up to 3 times. A review whose commit was superseded by a newer push, or whose PR was closed, is marked skipped.

Without `GITHUB_WEBHOOK_SECRET` the webhook endpoint answers `503` and only the catch-up runs. That works fine, but takes up to 30 minutes.

### Webhook setup (production)

1. Generate a secret: `openssl rand -hex 32`.
2. GitHub App settings → **Webhook**:
   - Tick **Active**.
   - **Webhook URL:** `https://<api-host>/api/webhooks/github`. Use the API host directly, not the static site.
   - **Webhook secret:** the value from step 1.
3. **Permissions & events → Subscribe to events:** tick **Pull request**.
4. Set `GITHUB_WEBHOOK_SECRET` on the Render API service to the same value.
5. In the app's **Advanced** tab, **Recent Deliveries** shows each delivery and its response. A `202` means queued.

### Webhook setup (local, via smee.io)

GitHub can't reach `localhost`, so a [smee.io](https://smee.io) channel relays deliveries:

1. <https://smee.io> → **Start a new channel**, and copy the URL.
2. On the **dev** app: Webhook **Active**, URL = the smee channel, a secret of your choice, and subscribe to **Pull request**.
3. Put the same secret in `backend/.env` as `GITHUB_WEBHOOK_SECRET`, and restart the backend.
4. Run the relay: `npx smee-client --url https://smee.io/<channel> --target http://localhost:8080/api/webhooks/github`.

---

## Release notes

**Releases** (also linked from each repo's page) drafts notes for everything merged since the last release:

1. **Base.** The repo's latest published release, else its highest semver tag, else nothing (all merged PRs).
2. **Changes.** PRs merged into the default branch after the base commit. The search API finds them, capped at 200.
3. **Groups**, decided by the first rule that matches:
   1. Dependabot or Renovate authors, or a `deps` scope, go to **Dependencies**.
   2. Otherwise labels decide, e.g. `enhancement` → **Features**, `bug` → **Fixes**, `chore`/`ci`/`docs` → **Internal**.
   3. Otherwise the title's conventional-commit prefix decides (`feat:`, `fix:`, `chore:`…).
   4. Anything else goes to **Other changes**.
4. **Version.** Breaking changes bump major (minor below 1.0): that means `!` after the prefix, `BREAKING CHANGE` in the body, or a breaking label. Features bump minor, and anything else bumps patch. The first release is `v0.1.0`. The tag's `v` style is kept.
5. **Notes.** Each entry is the PR title with its link and author. With an API key, the AI writes a short intro and a one-line summary per PR, and the links and authors stay.

You can edit the version and the notes, then **Create draft release**. That needs the repo's **Draft releases** switch (Settings → Repository automation, off by default) and uses a `contents: write` token limited to that repo. gitpulse **only ever creates drafts**: the tag isn't even created until you publish on GitHub. Drafts created are listed on the page, and recorded in `release_drafts`.

---

## Profile README

gitpulse can keep a block in your GitHub profile README (`<login>/<login>`) up to date:

- **Top languages**, by bytes, across your public, non-fork repos. Your Settings exclusions apply.
- **Recently active projects:** your 5 most recently pushed public repos, leaving out forks, archived repos and the profile repo itself.
- **Latest articles:** the 5 newest posts from your Medium RSS feed.

**Private repositories are never mentioned.** The block contains no timestamps, so it only changes when your data does.

**Setup:**

1. In your profile repo's README, add the two markers where the block should go:
   ```markdown
   <!-- gitpulse:start -->
   <!-- gitpulse:end -->
   ```
   gitpulse replaces **only** the text between them, keeping every other byte, including line endings. It refuses to run if a marker is missing, repeated or out of order.
2. **Settings → Profile README:**
   - Optionally enter your Medium feed URL. Only `https://medium.com/feed/@you`, `https://medium.com/feed/<publication>` or `https://<you>.medium.com/feed` are accepted. The fetch has a 10-second timeout and a 2 MB cap, and redirects off Medium are refused.
   - Press **Preview** to see the current block next to the new one.
3. Tick **Allow gitpulse to commit**, then **Save**. **Update README now** commits once, and only if the block changed. The commit message is `chore(gitpulse): refresh profile README stats`, and it uses a `contents: write` token limited to the profile repo.

`.github/workflows/profile-readme.yml` runs weekly on Mondays at 06:23 UTC, and can also be started manually. It does nothing for users who haven't turned the feature on. If the Medium feed is down, the update is skipped rather than removing the articles section. Each run is logged in `profile_readme_runs`, and the last one shows in the preview.

`PROFILE_README_REPO` overrides the target repo. Locally it should point at a sandbox repo, so testing never touches your real profile. The repo must be one of yours that a scan has seen.

---

## MCP server

gitpulse exposes its data to AI assistants over the [Model Context Protocol](https://modelcontextprotocol.io). It uses the official Go SDK, with streamable HTTP at **`https://<api-host>/mcp`**. Use the API host directly, not the static site.

| Tool | What it returns |
|---|---|
| `list_repos_needing_attention` | Lowest-scoring repos, worst first, with the checks each one misses (`limit`, `maxScore`, `includeArchived`) |
| `get_repo_health` | Score, full breakdown, open PRs/issues, stale branches and failed runs for one repo (`repo`) |
| `get_language_stats` | Language mix by bytes, and by year |
| `get_activity` | Weekly commits, streaks, busiest weekday/hour, top repos by commits (`weeks`) |
| `get_digest` | What changed since the previous scan |
| `get_pr_reviews` | Recent PR assistant reviews (`repo`, `limit`) |
| `draft_release_notes` | **Write.** Creates a *draft* release with notes since the last release (`repo`, optional `version`). Only exists when `MCP_ALLOW_WRITES=true`, and only works in repos whose **Draft releases** switch is on. It never publishes. |

All other tools are read-only and annotated as such.

**Security:**
- Requests need `Authorization: Bearer <MCP_TOKEN>`, compared in constant time.
- Without `MCP_TOKEN`, `/mcp` answers `503`.
- The token acts as one gitpulse user: `MCP_USER`, which defaults to the only entry in `ALLOWED_USERS`. That user must have signed in once.
- The server is stateless, so it survives the free instance sleeping. The first call after a sleep can take a minute.

**Setup:**

1. Generate a token: `openssl rand -hex 32`. Set it as `MCP_TOKEN` on the Render API service. Optionally set `MCP_ALLOW_WRITES=true`.
2. Connect a client:

**Claude Code** (speaks streamable HTTP directly):

```sh
claude mcp add --transport http --scope user gitpulse https://<api-host>/mcp \
  --header "Authorization: Bearer <MCP_TOKEN>"
```

Then ask, for example: "Which of my repos need attention?" Run `/mcp` inside Claude Code to check the connection.

**Claude Desktop** (launches local stdio servers): use the small proxy in `backend/cmd/gitpulse-mcp`. It connects to `/mcp`, mirrors the remote tool list, and retries for up to 3 minutes while a sleeping instance wakes.

```sh
go install github.com/harshit3011/gitpulse/backend/cmd/gitpulse-mcp@latest
# or, from a clone:  cd backend && go build -o gitpulse-mcp ./cmd/gitpulse-mcp
```

Edit `claude_desktop_config.json`: **Settings → Developer → Edit Config**. On Windows it's `%APPDATA%\Claude\claude_desktop_config.json`, on macOS `~/Library/Application Support/Claude/claude_desktop_config.json`.

```json
{
  "mcpServers": {
    "gitpulse": {
      "command": "C:\\Users\\<you>\\go\\bin\\gitpulse-mcp.exe",
      "env": {
        "GITPULSE_MCP_URL": "https://<api-host>/mcp",
        "GITPULSE_MCP_TOKEN": "<MCP_TOKEN>"
      }
    }
  }
}
```

Restart Claude Desktop. The gitpulse tools appear in the tools menu.

The token grants read access to your dashboard data, and with writes on, draft creation. Treat it like a password: don't commit config files that contain it. To revoke it, change `MCP_TOKEN` on Render.

---

## AI (optional)

gitpulse works fully without AI. Every AI feature has a rule-based fallback, and the UI shows a small "AI off" note where a summary would go. **No API key means no network calls, no errors, and $0.**

To turn AI on later, set `ANTHROPIC_API_KEY` on the API service and redeploy. Nothing else is needed: with `LLM_PROVIDER` unset, the key's presence turns AI on.

| Variable | Default | Purpose |
|---|---|---|
| `ANTHROPIC_API_KEY` | empty (AI off) | Anthropic API key |
| `LLM_PROVIDER` | unset (auto) | `none` keeps AI off even with a key; `anthropic` forces it on when a key exists |
| `LLM_MODEL` | `claude-haiku-5-5` | Model ID. Prices for Haiku/Sonnet/Opus 5.5 are built in. Other models need `LLM_PRICE_*` |
| `LLM_EFFORT` | `low` | Sent as `output_config.effort`, only to models known to support it |
| `LLM_MONTHLY_BUDGET_USD` | `5` | Hard monthly cap (UTC month); `0` never spends |
| `LLM_MAX_INPUT_TOKENS` / `LLM_MAX_OUTPUT_TOKENS` | `24000` / `4000` | Per-request limits |
| `LLM_PRICE_INPUT_PER_MTOK` / `LLM_PRICE_OUTPUT_PER_MTOK` | built in | Price override, USD per million tokens |

**Cost controls** (all in `backend/internal/llm`):

- **Budget cap with reservation.** Before each call, gitpulse takes a database lock and reserves the call's worst-case cost: estimated input plus the full output allowance. If that would push the month over the cap, it skips the call and uses the rule-based output. After the call, the reservation is replaced by the real cost from the API's reported usage. Scans and jobs never fail because of the cap.
- **Content-hash cache.** The cache key is the provider, model, prompt version, prompt, input and schema, so the same input is never sent twice. Cache hits cost $0 and still work when the budget is used up.
- **Size cap.** Inputs over `LLM_MAX_INPUT_TOKENS` are rejected before any call. Features trim their input to fit first. The 24k default stays well under Haiku 5.5's long-prompt tier, which charges more above 100k tokens.
- **Every call is logged** in `llm_calls`: feature, repo, tokens, cost and outcome. **Settings → AI usage** shows the month's spend.

Built-in prices were checked on 2026-11-23 against [Anthropic's pricing page](https://platform.claude.com/docs/en/about-claude/pricing). Haiku 5.5 is $0.10 input / $0.50 output per million tokens for prompts up to 100k tokens, and $0.50 / $2.50 above. `low` effort is supported on Haiku 5.5, whose default is `medium`.

**Untrusted content.** PR titles, bodies, diffs and commit messages are written by other people. gitpulse treats them as untrusted:

- **Fenced input.** The text goes in an `<untrusted>` block that the content can't close early. The system prompt tells the model to describe that block and ignore any instructions in it.
- **No tools.** The model is given no tools, so it can only return JSON matching a schema.
- **Cleaned output.** Before anything is displayed or posted, gitpulse removes links, images and HTML, neutralises `@mentions`, and caps the length.
- **Labels.** Suggested labels are filtered in code against the repo's real labels.

---

## Configuration reference

Every backend variable is documented in [`backend/.env.example`](backend/.env.example), and the frontend's one build variable is in [`frontend/.env.example`](frontend/.env.example).

| Variable | Required | Purpose |
|---|---|---|
| `DATABASE_URL` | yes | Postgres connection string |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | yes | GitHub App client ID (also the JWT issuer) and client secret |
| `GITHUB_APP_PRIVATE_KEY` | yes | The app's private key, base64-encoded PEM; mints installation tokens |
| `OAUTH_REDIRECT_URL` | yes | Must equal one of the app's callback URLs |
| `ALLOWED_USERS` | yes | Comma-separated GitHub logins allowed to sign in |
| `SCAN_SECRET` | yes | ≥24 chars; authorises `POST /api/scan` from Actions |
| `FRONTEND_URL` | no | Where to land after sign-in (`/` when same-origin) |
| `CORS_ALLOWED_ORIGINS` | no | Extra origins allowed to call the API with cookies |
| `COOKIE_SECURE` / `COOKIE_SAMESITE` | no | `true` / `lax` by default |
| `STATIC_DIR` | no | Serve a built frontend from the Go server |
| `PORT`, `LOG_LEVEL` | no | `8080`, `info` |
| `PROFILE_README_REPO` | no | Profile README target (default `<login>/<login>`); point at a sandbox locally |
| `GITHUB_WEBHOOK_SECRET` | no | Verifies webhook deliveries; without it only the 30-minute catch-up runs |
| `MCP_TOKEN` | no | Enables `/mcp` (≥32 chars, bearer token); see [MCP server](#mcp-server) |
| `MCP_USER` / `MCP_ALLOW_WRITES` | no | Which user the token acts as (default: the only allowlisted user) / enable `draft_release_notes` (default `false`) |
| `ANTHROPIC_API_KEY`, `LLM_*` | no | Optional AI; see [AI (optional)](#ai-optional) |

No secrets live in the repo: `.env` files are git-ignored, and production values live in Render and GitHub secrets.

## Operational notes

- **Rate limits:** scans batch work through GraphQL, send conditional REST requests (ETags cached in Postgres), wait when quota runs low, and honour `Retry-After` / secondary limits.
- **Rotating the app's private key:** generate a new key on the app page, update `GITHUB_APP_PRIVATE_KEY`, deploy, then delete the old key on GitHub. Nothing else is affected, because installation tokens are minted on demand.
- **App not installed or suspended:** scans fail with a clear error, and the dashboard shows an install banner.
- **Removing someone from `ALLOWED_USERS`** takes effect on their next request.
- **Limits of detection:** test and CI detection looks four directory levels deep. Commit stats cover default branches only and the last 52 weeks. GitHub doesn't let apps list stargazers, so the digest shows star *counts* per repo, not who starred.

## Extending

Each feature is its own package (`prreview`, `releases`, `profile`, `mcpserver`) built on three shared pieces:

- `internal/github` is the single GitHub access point.
- `internal/install` mints installation tokens, read-only or scoped to one repo and one permission.
- `internal/llm` is the only path to a model, with budget, cache and sanitising.

A new write feature should get a token through `install.RepoToken` and add a per-repo switch, which is off by default.
