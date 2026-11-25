package stats

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/harshit3011/gitpulse/backend/internal/insights"
)

const insightsSQL = `
WITH hist AS (
	SELECT
		ts.repo_id,
		COUNT(*)::int AS sample_count,
		COALESCE(EXTRACT(EPOCH FROM (MAX(ts.scanned_at) - MIN(ts.scanned_at))) / 86400.0, 0) AS span_days,
		COALESCE(REGR_SLOPE(ts.health_score::float8, EXTRACT(EPOCH FROM ts.scanned_at) / 86400.0), 0) AS health_slope_per_day,
		COALESCE(AVG(ts.open_prs::float8), 0) AS open_prs_mean,
		COALESCE(STDDEV_POP(ts.open_prs::float8), 0) AS open_prs_stddev,
		COALESCE(AVG(ts.commit_count_7d::float8), 0) AS commits_7d_mean,
		COALESCE(STDDEV_POP(ts.commit_count_7d::float8), 0) AS commits_7d_stddev,
		COALESCE(AVG(ts.failed_runs::float8), 0) AS failed_runs_mean,
		COALESCE(STDDEV_POP(ts.failed_runs::float8), 0) AS failed_runs_stddev
	FROM repo_trend_snapshots ts
	JOIN repos r ON r.id = ts.repo_id
	WHERE r.user_id = $1 AND ts.scanned_at >= $3::timestamptz - interval '90 days'
	GROUP BY ts.repo_id
),
pr_stats AS (
	SELECT
		i.repo_id,
		COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY GREATEST(0, EXTRACT(EPOCH FROM ($3::timestamptz - i.created_at)) / 86400.0)), 0) AS pr_median_age_days,
		COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY GREATEST(0, EXTRACT(EPOCH FROM ($3::timestamptz - i.created_at)) / 86400.0)), 0) AS pr_p90_age_days,
		COALESCE(100.0 * COUNT(*) FILTER (WHERE i.updated_at < $3::timestamptz - interval '14 days') / NULLIF(COUNT(*), 0), 0) AS pr_stale_pct
	FROM repo_items i
	WHERE i.scan_id = $2 AND i.kind = 'pr'
	GROUP BY i.repo_id
)
SELECT
	s.repo_id,
	r.full_name,
	r.source,
	s.status,
	s.has_ci,
	s.health_score::int,
	COALESCE(h.sample_count, 0),
	COALESCE(h.span_days, 0),
	COALESCE(h.health_slope_per_day, 0),
	s.open_prs,
	COALESCE(h.open_prs_mean, 0),
	COALESCE(h.open_prs_stddev, 0),
	COALESCE(p.pr_median_age_days, 0),
	COALESCE(p.pr_p90_age_days, 0),
	COALESCE(p.pr_stale_pct, 0),
	COALESCE(cur.commit_count_7d, 0),
	COALESCE(h.commits_7d_mean, 0),
	COALESCE(h.commits_7d_stddev, 0),
	COALESCE(cur.commit_count_30d, 0),
	COALESCE(cur.unique_authors_30d, 0),
	COALESCE(cur.top_author_login, ''),
	COALESCE(cur.top_author_pct::int, 0),
	COALESCE(cur.failed_runs, 0),
	COALESCE(h.failed_runs_mean, 0),
	COALESCE(h.failed_runs_stddev, 0)
FROM repo_snapshots s
JOIN repos r ON r.id = s.repo_id
LEFT JOIN repo_trend_snapshots cur ON cur.scan_id = s.scan_id AND cur.repo_id = s.repo_id
LEFT JOIN hist h ON h.repo_id = s.repo_id
LEFT JOIN pr_stats p ON p.repo_id = s.repo_id
WHERE s.scan_id = $2 AND r.user_id = $1`

func scanRawInsights(row pgx.CollectableRow) (insights.RawRepoStats, error) {
	var r insights.RawRepoStats
	err := row.Scan(
		&r.RepoID, &r.FullName, &r.Source, &r.Status, &r.HasCI, &r.HealthScore,
		&r.SampleCount, &r.SpanDays, &r.HealthSlopePerDay,
		&r.OpenPRs, &r.OpenPRsMean, &r.OpenPRsStdDev,
		&r.PRMedianAgeDays, &r.PRP90AgeDays, &r.PRStalePct,
		&r.CommitCount7d, &r.Commits7dMean, &r.Commits7dStdDev,
		&r.CommitCount30d, &r.UniqueAuthors30d, &r.TopAuthorLogin, &r.TopAuthorPct,
		&r.FailedRuns, &r.FailedRunsMean, &r.FailedRunsStdDev,
	)
	return r, err
}

// Insights computes Postgres-backed statistical insights for all repositories
// in the latest succeeded scan, optionally filtered by source ("owned" or "watched").
func (s *Service) Insights(ctx context.Context, userID int64, source string) ([]insights.RepoInsights, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var (
		rows pgx.Rows
		qerr error
	)
	if source == "owned" || source == "watched" {
		rows, qerr = s.db.Query(ctx, insightsSQL+` AND r.source = $4 ORDER BY lower(r.full_name)`, userID, latest.ID, now, source)
	} else {
		rows, qerr = s.db.Query(ctx, insightsSQL+` ORDER BY lower(r.full_name)`, userID, latest.ID, now)
	}
	if qerr != nil {
		return nil, qerr
	}
	raws, err := pgx.CollectRows(rows, scanRawInsights)
	if err != nil {
		return nil, err
	}
	out := make([]insights.RepoInsights, 0, len(raws))
	for _, r := range raws {
		out = append(out, insights.Compute(r))
	}
	return out, nil
}

// RepoInsights computes Postgres-backed statistical insights for a single repository.
func (s *Service) RepoInsights(ctx context.Context, userID, repoID int64) (*insights.RepoInsights, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rows, err := s.db.Query(ctx, insightsSQL+` AND s.repo_id = $4`, userID, latest.ID, now, repoID)
	if err != nil {
		return nil, err
	}
	raws, err := pgx.CollectRows(rows, scanRawInsights)
	if err != nil {
		return nil, err
	}
	if len(raws) == 0 {
		return nil, ErrNotFound
	}
	res := insights.Compute(raws[0])
	return &res, nil
}

var _ = errors.Is
