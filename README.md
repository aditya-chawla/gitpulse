# gitpulse

A personal GitHub dashboard. It scans every repository you own and shows:

- **Analytics**: language mix (overall and per repo), language trend by year, weekly commits, when you commit (day × hour, in your timezone), streaks, top repos by stars, forks and your commits, and the detected tech stack.
- **Repo health**: a 0–100 score per repo with a visible breakdown (README, license, CI, tests, recent activity, stale branches, old PRs, description/topics), an active/slowing/dormant status, and a sortable, filterable table with a detail page per repo.
- **Digest**: PRs waiting on you, new issues, failed workflow runs, and new stars/forks since the previous scan.

The dashboard reads only from Postgres. A **scan** fetches everything from GitHub and stores it as a snapshot, so you get trends over time. Scans run daily from GitHub Actions, or on demand from the **Scan now** button.

```
backend/    Go 1.27 · chi · pgx · embedded SQL migrations
  internal/github    GitHub client (interface + GraphQL/REST implementation)
  internal/scanner   background scans → snapshots
  internal/detect    README/CI/tests + tech-stack detection
  internal/health    health score (pure, unit-tested)
  internal/stats     dashboard queries + aggregations (unit-tested)
  internal/auth      GitHub OAuth, allowlist, sessions, token encryption
frontend/   React · TypeScript · Vite · Tailwind · Recharts
.github/workflows/  ci.yml (vet, test, lint, typecheck, build) · scan.yml (daily scan)
render.yaml         Render Blueprint (API + static site)
docker-compose.yml  local Postgres + backend + frontend
```

---

## Local setup

**Prerequisites:** Docker Desktop. Go 1.27+ and Node 24+ are only needed to run things outside Docker.

1. **Create a GitHub OAuth app for local use** (see [GitHub OAuth app](#github-oauth-app)), with callback URL
   `http://localhost:8080/api/auth/callback`.
2. **Configure the backend:**
   ```sh
   cp backend/.env.example backend/.env
   ```
   Fill in `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `TOKEN_ENC_KEY` and `SCAN_SECRET`. The other defaults already suit local use. Generate the secrets with:
   ```sh
   openssl rand -base64 32   # TOKEN_ENC_KEY
   openssl rand -hex 32      # SCAN_SECRET
   ```
   (PowerShell: `[Convert]::ToBase64String((1..32|%{[byte](Get-Random -Max 256)}))`.)
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

## GitHub OAuth app

An OAuth app allows exactly **one** callback URL, so create **two apps**: one for local development and one for production.

1. Go to <https://github.com/settings/developers> → **OAuth Apps** → **New OAuth App**.
2. Fill in:

   | Field | Local app | Production app |
   |---|---|---|
   | Application name | `gitpulse (local)` | `gitpulse` |
   | Homepage URL | `http://localhost:5173` | `https://<your-static-site>.onrender.com` |
   | Authorization callback URL | `http://localhost:8080/api/auth/callback` | `https://<your-static-site>.onrender.com/api/auth/callback` |

   The production callback goes through the **frontend** host. Render rewrites `/api/*` to the backend, so the session cookie belongs to the site you actually browse.
3. Leave "Enable Device Flow" off. Click **Register application**, then **Generate a new client secret**.
4. Put the Client ID and secret into `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET`: in `backend/.env` locally, in the Render dashboard for production.

On first sign-in GitHub asks for the `repo` and `read:user` scopes. `repo` is required to read private repositories and Actions runs; OAuth apps have no read-only variant. gitpulse only ever reads. The token is stored encrypted with AES-256-GCM using `TOKEN_ENC_KEY`.

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
   - `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`: from the **production** OAuth app. If you haven't made it yet, enter placeholders and fix them in step 6.
   - `OAUTH_REDIRECT_URL`: `https://<static-site-host>/api/auth/callback`.
   - `CORS_ALLOWED_ORIGINS`: `https://<static-site-host>`.

   Service hostnames are global, so if `gitpulse` or `gitpulse-api` is taken, Render adds a suffix. You'll see the real hosts after creation; placeholders are fine for now.
   `TOKEN_ENC_KEY` and `SCAN_SECRET` are generated by Render.
4. Click **Apply**. When the services exist, note both URLs (e.g. `https://gitpulse-xxxx.onrender.com` and `https://gitpulse-api-xxxx.onrender.com`).
5. **If the API host isn't exactly `gitpulse-api.onrender.com`**, edit the rewrite `destination` in `render.yaml` to the real host and push. The static site redeploys.
6. Fix the values that depend on hosts: in the API service's **Environment** tab, set `OAUTH_REDIRECT_URL` and `CORS_ALLOWED_ORIGINS` to the real static-site host, and update the production OAuth app's Homepage and callback URLs to match.
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
3. Use the API's own host everywhere: OAuth callback `https://<api-host>/api/auth/callback`, `CORS_ALLOWED_ORIGINS` empty.

### Cross-origin setup (not recommended)

To keep two separate hosts without a rewrite: build the frontend with `VITE_API_BASE_URL=https://<api-host>`, and set `CORS_ALLOWED_ORIGINS=https://<static-site-host>`, `COOKIE_SAMESITE=none` and `COOKIE_SECURE=true` on the API. Browsers that block third-party cookies (Safari, and others by setting) will then fail to keep you signed in, which is why same-origin is the default.

---

## Daily scan (GitHub Actions)

Free Render services sleep after 15 idle minutes, so the app has no in-process scheduler. `.github/workflows/scan.yml` runs daily at 02:17 UTC (and on demand via **Actions → Daily scan → Run workflow**). It:

1. retries `/healthz` for up to ~15 minutes to survive a cold start,
2. calls `POST /api/scan` with the `X-Scan-Secret` header (scans every allowlisted user who has signed in once),
3. polls `GET /api/scans/{id}` every 30 s until the scan finishes. This keeps the instance awake and fails the workflow if the scan fails.

**Repository secrets** (GitHub → repo → Settings → Secrets and variables → Actions → New repository secret):

| Secret | Value |
|---|---|
| `GITPULSE_API_URL` | The API's public URL, e.g. `https://gitpulse-api.onrender.com` (no trailing slash). In the single-service fallback, the one service's URL. |
| `SCAN_SECRET` | Copy the `SCAN_SECRET` value from the Render API service's Environment tab. |

CI (`ci.yml`) needs no secrets.

---

## Configuration reference

Every backend variable is documented in [`backend/.env.example`](backend/.env.example), and the frontend's one build variable is in [`frontend/.env.example`](frontend/.env.example).

| Variable | Required | Purpose |
|---|---|---|
| `DATABASE_URL` | yes | Postgres connection string |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | yes | OAuth app credentials |
| `OAUTH_REDIRECT_URL` | yes | Must equal the OAuth app's callback URL |
| `ALLOWED_USERS` | yes | Comma-separated GitHub logins allowed to sign in |
| `TOKEN_ENC_KEY` | yes | base64 of 32 bytes; encrypts stored GitHub tokens |
| `SCAN_SECRET` | yes | ≥24 chars; authorises `POST /api/scan` from Actions |
| `FRONTEND_URL` | no | Where to land after sign-in (`/` when same-origin) |
| `CORS_ALLOWED_ORIGINS` | no | Extra origins allowed to call the API with cookies |
| `COOKIE_SECURE` / `COOKIE_SAMESITE` | no | `true` / `lax` by default |
| `STATIC_DIR` | no | Serve a built frontend from the Go server |
| `PORT`, `LOG_LEVEL` | no | `8080`, `info` |

No secrets live in the repo: `.env` files are git-ignored, and production values live in Render and GitHub secrets.

## Operational notes

- **Rate limits:** scans batch work through GraphQL, send conditional REST requests (ETags cached in Postgres), wait when quota runs low, and honour `Retry-After` / secondary limits.
- **Rotating `TOKEN_ENC_KEY`:** stored tokens can no longer be decrypted. Sign in again to re-store yours. Scans fail with a clear error until you do.
- **Removing someone from `ALLOWED_USERS`** takes effect on their next request.
- **Limits of detection:** test and CI detection looks four directory levels deep. Commit stats cover default branches only and the last 52 weeks.

## Extending

The v2 ideas (AI PR summaries, release notes, profile README updater, MCP server) slot in without restructuring. `internal/github.Client` is the single GitHub access point, and `internal/stats.Service` is the single read model. A new feature adds an endpoint or job that uses one or both.
