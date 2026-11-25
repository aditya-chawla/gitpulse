# frontend/src/pages

Lazy-loaded route pages for `gitpulse`.

## Files

- `Login.tsx` — Sign-in card with **"Sign in with GitHub"** and **"View live demo (read-only)"** buttons.
- `Analytics.tsx` — Maintainer overview (`My Repos`): **What to fix next**, summary tiles, language mix/trends, weekly commits, commit heatmap, tech stack, and history charts.
- `Health.tsx` — Sortable, filterable health table for owned repositories with expandable score breakdowns and trend badges.
- `Watched.tsx` — **`OSS Explorer`** dashboard: add/remove public `owner/repo` targets, view Contributor Readiness, commit cadence, diversity score, PR responsiveness, Z-score anomalies, **`Explain ✦`** buttons, and read-only project signals.
- `Repo.tsx` — Single-repository deep-dive: score breakdown, Postgres **Data Science signals** card + `Explain ✦`, 7d/30d deltas, languages, tech stack, weekly commits, branches, open PRs/issues, and failed CI runs.
- `Digest.tsx` — Changes since the previous scan (PRs waiting on you, new issues, failed runs, star/fork deltas, and top maintenance alerts).
- `PRs.tsx` — PR assistant review history, risky-change flags, label suggestions, and manual sync trigger.
- `Releases.tsx` — Release notes previewer, semver bump selector, and GitHub *draft* release creator.
- `Settings.tsx` — GitHub App status, watchlist management, per-repo automation switches, Profile README sync, AI usage, and aggregate exclusions.
