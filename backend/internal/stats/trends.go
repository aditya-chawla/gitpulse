package stats

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/harshit3011/gitpulse/backend/internal/trends"
)

type ActionItem struct {
	ID        int64           `json:"id"`
	RepoID    int64           `json:"repoId"`
	RepoName  string          `json:"repoName"`
	Rule      string          `json:"rule"`
	Severity  int             `json:"severity"`
	Title     string          `json:"title"`
	Detail    json.RawMessage `json:"detail"`
	URL       string          `json:"url"`
	Active    bool            `json:"active"`
	Dismissed bool            `json:"dismissed"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type RepoTrend struct {
	ID        int64            `json:"id"`
	Name      string           `json:"name"`
	FullName  string           `json:"fullName"`
	Direction trends.Direction `json:"direction"`
	Delta7d   *trends.DeltaSet `json:"delta7d,omitempty"`
	Delta30d  *trends.DeltaSet `json:"delta30d,omitempty"`
}

type RepoTrendDetail struct {
	RepoTrend
	Alerts  []ActionItem      `json:"alerts"`
	History []trends.Snapshot `json:"history"`
}

func (s *Service) ActionItems(ctx context.Context, userID int64, limit int) ([]ActionItem, error) {
	return s.ActionItemsForSource(ctx, userID, limit, "owned")
}

func (s *Service) ActionItemsForSource(ctx context.Context, userID int64, limit int, source string) ([]ActionItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var (
		rows pgx.Rows
		err  error
	)
	if source == "owned" || source == "watched" {
		rows, err = s.db.Query(ctx, `
			SELECT a.id, a.repo_id, r.full_name, a.rule, a.severity, a.title, a.detail,
			       coalesce(a.url,''), a.active, a.dismissed, a.created_at, a.updated_at
			FROM action_items a
			JOIN repos r ON r.id = a.repo_id
			WHERE a.user_id = $1 AND a.active = true AND a.dismissed = false AND r.source = $3
			ORDER BY a.severity DESC, a.updated_at DESC
			LIMIT $2`, userID, limit, source)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT a.id, a.repo_id, r.full_name, a.rule, a.severity, a.title, a.detail,
			       coalesce(a.url,''), a.active, a.dismissed, a.created_at, a.updated_at
			FROM action_items a
			JOIN repos r ON r.id = a.repo_id
			WHERE a.user_id = $1 AND a.active = true AND a.dismissed = false
			ORDER BY a.severity DESC, a.updated_at DESC
			LIMIT $2`, userID, limit)
	}
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, scanActionItem)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ActionItem{}
	}
	return items, nil
}

func (s *Service) DismissActionItem(ctx context.Context, userID, id int64) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE action_items SET dismissed = true, updated_at = now()
		WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanActionItem(r pgx.CollectableRow) (ActionItem, error) {
	var a ActionItem
	err := r.Scan(&a.ID, &a.RepoID, &a.RepoName, &a.Rule, &a.Severity, &a.Title, &a.Detail,
		&a.URL, &a.Active, &a.Dismissed, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Service) loadTrendBaseline(ctx context.Context, repoID, scanID int64, at time.Time) (cur trends.Snapshot, base trends.Baseline, err error) {
	querySnap := func(where string, args ...any) (*trends.Snapshot, error) {
		var ts trends.Snapshot
		e := s.db.QueryRow(ctx, `
			SELECT repo_id, scan_id, scanned_at, health_score, open_prs, open_issues,
			       stale_branches, failed_runs, commit_count_7d, commit_count_30d,
			       unique_authors_30d, coalesce(top_author_login,''), top_author_pct, stars, forks
			FROM repo_trend_snapshots `+where+` LIMIT 1`, args...).Scan(
			&ts.RepoID, &ts.ScanID, &ts.ScannedAt, &ts.HealthScore, &ts.OpenPRs, &ts.OpenIssues,
			&ts.StaleBranches, &ts.FailedRuns, &ts.CommitCount7d, &ts.CommitCount30d,
			&ts.UniqueAuthors30d, &ts.TopAuthorLogin, &ts.TopAuthorPct, &ts.Stars, &ts.Forks)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, nil
		}
		return &ts, e
	}

	c, err := querySnap(`WHERE repo_id = $1 AND scan_id = $2`, repoID, scanID)
	if err != nil {
		return cur, base, err
	}
	if c == nil {
		return cur, base, ErrNotFound
	}
	cur = *c
	if base.PrevScan, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 ORDER BY scan_id DESC`, repoID, scanID); err != nil {
		return cur, base, err
	}
	if base.Days7, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 AND scanned_at <= $3 ORDER BY scanned_at DESC`, repoID, scanID, at.Add(-7*24*time.Hour)); err != nil {
		return cur, base, err
	}
	if base.Days30, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 AND scanned_at <= $3 ORDER BY scanned_at DESC`, repoID, scanID, at.Add(-30*24*time.Hour)); err != nil {
		return cur, base, err
	}
	return cur, base, nil
}

func (s *Service) TrendsOverview(ctx context.Context, userID int64) ([]RepoTrend, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	repos, err := s.repoRows(ctx, latest.ID, st)
	if err != nil {
		return nil, err
	}
	return s.trendsForRepos(ctx, userID, latest.ID, latest.StartedAt, repos)
}

func (s *Service) trendsForRepos(ctx context.Context, userID, scanID int64, scannedAt time.Time, repos []RepoRow) ([]RepoTrend, error) {
	// Preload active high-severity rules per repo so direction accounts for them.
	highSev := map[int64]bool{}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT repo_id FROM action_items
		WHERE user_id = $1 AND active = true AND severity >= 4`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err == nil {
			highSev[rid] = true
		}
	}
	rows.Close()

	out := make([]RepoTrend, 0, len(repos))
	for _, r := range repos {
		if r.Excluded != "" {
			continue
		}
		rt := RepoTrend{ID: r.ID, Name: r.Name, FullName: r.FullName, Direction: trends.Steady}
		cur, base, err := s.loadTrendBaseline(ctx, r.ID, scanID, scannedAt)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				out = append(out, rt)
				continue
			}
			return nil, err
		}
		tr := trends.Evaluate(cur, base, trends.RepoContext{FullName: r.FullName, IsArchived: true}, cur.ScannedAt)
		rt.Delta7d = tr.Delta7d
		rt.Delta30d = tr.Delta30d
		rt.Direction = tr.Direction
		if highSev[r.ID] {
			rt.Direction = trends.Slipping
		}
		out = append(out, rt)
	}
	return out, nil
}

func (s *Service) TrendForRepo(ctx context.Context, userID, repoID int64) (*RepoTrendDetail, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	var name, fullName string
	err = s.db.QueryRow(ctx, `SELECT split_part(full_name,'/',2), full_name FROM repos WHERE id = $1 AND user_id = $2`,
		repoID, userID).Scan(&name, &fullName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	cur, base, err := s.loadTrendBaseline(ctx, repoID, latest.ID, latest.StartedAt)
	if err != nil {
		return nil, err
	}
	tr := trends.Evaluate(cur, base, trends.RepoContext{FullName: fullName, IsArchived: true}, cur.ScannedAt)

	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.repo_id, $3::text, a.rule, a.severity, a.title, a.detail,
		       coalesce(a.url,''), a.active, a.dismissed, a.created_at, a.updated_at
		FROM action_items a
		WHERE a.user_id = $1 AND a.repo_id = $2 AND a.active = true
		ORDER BY a.severity DESC, a.updated_at DESC`, userID, repoID, fullName)
	if err != nil {
		return nil, err
	}
	alerts, err := pgx.CollectRows(rows, scanActionItem)
	if err != nil {
		return nil, err
	}
	if alerts == nil {
		alerts = []ActionItem{}
	}
	dir := tr.Direction
	for _, a := range alerts {
		if a.Severity >= 4 {
			dir = trends.Slipping
			break
		}
	}

	hRows, err := s.db.Query(ctx, `
		SELECT repo_id, scan_id, scanned_at, health_score, open_prs, open_issues,
		       stale_branches, failed_runs, commit_count_7d, commit_count_30d,
		       unique_authors_30d, coalesce(top_author_login,''), top_author_pct, stars, forks
		FROM repo_trend_snapshots
		WHERE repo_id = $1
		ORDER BY scanned_at ASC
		LIMIT 90`, repoID)
	if err != nil {
		return nil, err
	}
	hist, err := pgx.CollectRows(hRows, func(r pgx.CollectableRow) (trends.Snapshot, error) {
		var ts trends.Snapshot
		err := r.Scan(&ts.RepoID, &ts.ScanID, &ts.ScannedAt, &ts.HealthScore, &ts.OpenPRs, &ts.OpenIssues,
			&ts.StaleBranches, &ts.FailedRuns, &ts.CommitCount7d, &ts.CommitCount30d,
			&ts.UniqueAuthors30d, &ts.TopAuthorLogin, &ts.TopAuthorPct, &ts.Stars, &ts.Forks)
		return ts, err
	})
	if err != nil {
		return nil, err
	}
	if hist == nil {
		hist = []trends.Snapshot{}
	}

	return &RepoTrendDetail{
		RepoTrend: RepoTrend{
			ID:        repoID,
			Name:      name,
			FullName:  fullName,
			Direction: dir,
			Delta7d:   tr.Delta7d,
			Delta30d:  tr.Delta30d,
		},
		Alerts:  alerts,
		History: hist,
	}, nil
}
