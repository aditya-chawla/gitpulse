# backend/internal/stats

Dashboard query layer, aggregations, Postgres 17 Data Science SQL, trends, and action items.

## Files

- `queries.go` — `Overview`, `Analytics`, `ReposForSource`, `Repo` detail, `Digest`, and user `Settings` queries (with `Overview`, `Analytics`, and `Digest` scoped strictly to `owned` repos).
- `aggregate.go` — Pure Go aggregations for language shares, yearly language trends, weekly commit charts, streaks, and timezone-aware commit heatmaps.
- `insights.go` — Postgres 17 statistical SQL query (`REGR_SLOPE`, `AVG`, `STDDEV_POP`, `PERCENTILE_CONT`) feeding `internal/insights`.
- `trends.go` — `TrendsOverview`, `TrendForRepo`, `ActionItemsForSource`, and `DismissActionItem`.
- `aggregate_test.go` — Unit tests for language, weekly, heatmap, and streak calculations.
