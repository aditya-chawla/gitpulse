# frontend

React 19 + TypeScript + Vite + Tailwind CSS 4 + Recharts single-page application for `gitpulse`.

## Files

- `index.html` — HTML shell and page title/favicon entrypoint.
- `package.json` / `package-lock.json` — Dependencies (`react`, `react-router`, `recharts`) and scripts (`dev`, `lint`, `typecheck`, `build`).
- `vite.config.ts` — Vite dev server configuration with `/api` proxy to the Go backend.
- `tsconfig.json` / `tsconfig.app.json` / `tsconfig.node.json` — TypeScript project references and strict compiler settings.
- `.oxlintrc.json` — `oxlint` linter ruleset.
- `.env.example` — Optional frontend environment variable template (`VITE_API_BASE_URL`).
- `.gitignore` — Ignores `node_modules/` and `dist/`.

## Subfolders

- `public/` — Static assets served verbatim (`favicon.svg`).
- `src/` — Application source code, API client, demo dataset, components, and pages.
