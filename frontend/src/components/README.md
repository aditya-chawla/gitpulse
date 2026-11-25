# frontend/src/components

Shared UI components, Recharts visualizations, layout shell, and context providers.

## Files

- `Layout.tsx` — Sticky header with the **`My Repos` | `OSS Explorer`** mode switcher, mode-specific nav links, **`✦ Ask AI`** toggle, scan progress button, theme toggle, user menu, and read-only demo banner.
- `DataProvider.tsx` — Provides global cache invalidation (`invalidate()`) when scans complete or settings change.
- `AdvisorContext.tsx` — Manages `Ask AI ✦` open state, active repo scope, BYOK provider/key persistence in `localStorage`, and message history.
- `AdvisorPanel.tsx` — Context-aware right-hand side panel (`Maintainer Copilot` vs `Contributor Advisor`), BYOK settings, and copyable MCP config.
- `ActionQueue.tsx` — **"What to fix next"** severity-weighted maintenance alert card with persistent dismissal.
- `charts.tsx` — Recharts components (`BarList`, `LanguageTrendChart`, `WeeklyChart`, `Heatmap`, `HistoryChart`, `SegmentedBar`, `LanguageTable`).
- `ui.tsx` — Reusable UI primitives (`Card`, `StatTile`, `ScoreMeter`, `StatusBadge`, `TrendBadge`, `Pill`, `Table`, `Spinner`, `ErrorBox`, `Empty`, `NoScanYet`, `AiOffNote`).
