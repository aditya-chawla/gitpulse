# frontend/src

Core frontend application code for `gitpulse`.

## Files

- `main.tsx` — React root mount with `BrowserRouter`.
- `App.tsx` — Top-level routing (`My Repos` + `OSS Explorer`), auth guard, demo mode entry/exit, and lazy-loaded pages.
- `api.ts` — Typed REST client (`api()`), error types (`AuthError`, `ApiError`), demo-mode interceptor, and TypeScript interfaces mirroring backend responses.
- `demo.ts` — Client-side read-only Live Demo dataset (`?demo=1`) and deterministic advisor responder.
- `data.ts` — `useApi` data-fetching hook with in-memory cache and global `invalidate()` version counter.
- `advisor.ts` — `AdvisorContext` definition and `useAdvisor()` hook for opening/scoping the `Ask AI ✦` side panel.
- `colors.ts` — Deterministic language color palette and health-score color helpers.
- `format.ts` — Date, relative time (`ago`), number, byte, percentage, and weekday formatting helpers.
- `index.css` — Tailwind CSS v4 import and light/dark CSS theme variables.

## Subfolders

- `components/` — Reusable UI primitives, charts, layout shell, action queue, and AI advisor panel.
- `pages/` — Route-level views for Maintainer Mode, OSS Explorer, Repo Detail, Settings, and Login.
