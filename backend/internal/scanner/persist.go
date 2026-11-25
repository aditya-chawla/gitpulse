package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/detect"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/health"
	"github.com/harshit3011/gitpulse/backend/internal/trends"
)

// Scan is a row of the scans table, as exposed to the API.
type Scan struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"userId"`
	Trigger       string     `json:"trigger"`
	Status        string     `json:"status"`
	Phase         string     `json:"phase"`
	ReposTotal    int        `json:"reposTotal"`
	ReposDone     int        `json:"reposDone"`
	APICalls      int        `json:"apiCalls"`
	RateRemaining *int       `json:"rateRemaining"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
}

const scanCols = `id, user_id, trigger, status, coalesce(phase,''), repos_total, repos_done, api_calls,
	rate_remaining, coalesce(error,''), started_at, finished_at`

func scanRow(row pgx.Row) (*Scan, error) {
	var s Scan
	err := row.Scan(&s.ID, &s.UserID, &s.Trigger, &s.Status, &s.Phase, &s.ReposTotal, &s.ReposDone,
		&s.APICalls, &s.RateRemaining, &s.Error, &s.StartedAt, &s.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

var ErrNotFound = errors.New("scan not found")

func GetScan(ctx context.Context, db *pgxpool.Pool, id int64) (*Scan, error) {
	return scanRow(db.QueryRow(ctx, `SELECT `+scanCols+` FROM scans WHERE id = $1`, id))
}

func LatestScan(ctx context.Context, db *pgxpool.Pool, userID int64) (*Scan, error) {
	return scanRow(db.QueryRow(ctx, `SELECT `+scanCols+` FROM scans WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, userID))
}

// insertScan creates a running scan, or returns the already-running one.
func insertScan(ctx context.Context, db *pgxpool.Pool, userID int64, trigger string) (id int64, existing bool, err error) {
	err = db.QueryRow(ctx, `INSERT INTO scans (user_id, trigger, status, phase) VALUES ($1, $2, 'running', 'starting') RETURNING id`,
		userID, trigger).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // scans_one_running
		err = db.QueryRow(ctx, `SELECT id FROM scans WHERE user_id = $1 AND status = 'running'`, userID).Scan(&id)
		return id, true, err
	}
	return id, false, err
}

func setPhase(ctx context.Context, db *pgxpool.Pool, id int64, phase string, total int) error {
	_, err := db.Exec(ctx, `UPDATE scans SET phase = $2, repos_total = GREATEST(repos_total, $3) WHERE id = $1`, id, phase, total)
	return err
}

func repoDone(ctx context.Context, db *pgxpool.Pool, id int64, rate github.RateInfo) error {
	_, err := db.Exec(ctx, `UPDATE scans SET repos_done = repos_done + 1, api_calls = $2, rate_remaining = $3 WHERE id = $1`,
		id, rate.Calls, rate.GraphQLRemaining)
	return err
}

func finishScan(ctx context.Context, db *pgxpool.Pool, id int64, status, errMsg string, rate github.RateInfo) error {
	_, err := db.Exec(ctx, `
		UPDATE scans SET status = $2, error = NULLIF($3, ''), finished_at = now(), phase = $2,
		  api_calls = $4, rate_remaining = $5
		WHERE id = $1`, id, status, errMsg, rate.Calls, rate.GraphQLRemaining)
	return err
}

// RecoverStale marks scans left 'running' by a previous process as failed.
func RecoverStale(ctx context.Context, db *pgxpool.Pool) (int64, error) {
	tag, err := db.Exec(ctx, `UPDATE scans SET status = 'failed', finished_at = now(), phase = 'failed',
		error = 'interrupted: server restarted during scan' WHERE status = 'running'`)
	return tag.RowsAffected(), err
}

func upsertRepos(ctx context.Context, db *pgxpool.Pool, userID, scanID int64, repos []github.Repo) error {
	ids := make([]int64, len(repos))
	names := make([]string, len(repos))
	for i, r := range repos {
		ids[i], names[i] = r.ID, r.Ref.String()
	}
	_, err := db.Exec(ctx, `
		INSERT INTO repos (id, user_id, full_name, last_seen_scan_id, source)
		SELECT unnest($1::bigint[]), $3, unnest($2::text[]), $4, 'owned'
		ON CONFLICT (id) DO UPDATE SET full_name = EXCLUDED.full_name, last_seen_scan_id = EXCLUDED.last_seen_scan_id, source = 'owned', watched_repo_id = NULL`,
		ids, names, userID, scanID)
	return err
}

type watchedTarget struct {
	ID    int64
	Owner string
	Name  string
}

func listWatched(ctx context.Context, db *pgxpool.Pool, userID int64) ([]watchedTarget, error) {
	rows, err := db.Query(ctx, `SELECT id, owner, name FROM watched_repos WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (watchedTarget, error) {
		var w watchedTarget
		return w, r.Scan(&w.ID, &w.Owner, &w.Name)
	})
}

func setWatchedError(ctx context.Context, db *pgxpool.Pool, id int64, errMsg string) error {
	_, err := db.Exec(ctx, `UPDATE watched_repos SET last_error = NULLIF($2, '') WHERE id = $1`, id, errMsg)
	return err
}

func markWatchedScanned(ctx context.Context, db *pgxpool.Pool, id, githubID int64) error {
	_, err := db.Exec(ctx, `UPDATE watched_repos SET github_id = $2, last_error = NULL WHERE id = $1`, id, githubID)
	return err
}

func upsertWatchedRepos(ctx context.Context, db *pgxpool.Pool, userID, scanID int64, repos []github.Repo, watchedIDs []int64) error {
	ids := make([]int64, len(repos))
	names := make([]string, len(repos))
	for i, r := range repos {
		ids[i], names[i] = r.ID, r.Ref.String()
	}
	_, err := db.Exec(ctx, `
		INSERT INTO repos (id, user_id, full_name, last_seen_scan_id, source, watched_repo_id)
		SELECT unnest($1::bigint[]), $3, unnest($2::text[]), $4, 'watched', unnest($5::bigint[])
		ON CONFLICT (id) DO UPDATE SET
		  full_name = EXCLUDED.full_name,
		  last_seen_scan_id = EXCLUDED.last_seen_scan_id,
		  source = CASE WHEN repos.user_id = EXCLUDED.user_id THEN 'watched' ELSE repos.source END,
		  watched_repo_id = CASE WHEN repos.user_id = EXCLUDED.user_id THEN EXCLUDED.watched_repo_id ELSE repos.watched_repo_id END`,
		ids, names, userID, scanID, watchedIDs)
	return err
}

func lastCommitTime(ctx context.Context, db *pgxpool.Pool, repoID int64) (*time.Time, error) {
	var t *time.Time
	err := db.QueryRow(ctx, `SELECT max(authored_at) FROM commits WHERE repo_id = $1`, repoID).Scan(&t)
	return t, err
}

// repoResult is everything gathered for one repo in one scan.
type repoResult struct {
	Repo      github.Repo
	Files     detect.Files
	Tech      []detect.Tech
	Weeks     []github.CommitWeek
	Commits   []github.Commit
	Runs      []github.WorkflowRun
	Health    health.Result
	TrendSnap trends.Snapshot
	TrendCtx  trends.RepoContext
	Login     string // viewer login, for review-request/assignee flags
	UserID    int64
	ScanID    int64
}

func contains(list []string, s string) bool { return slices.Contains(list, s) }

// writeRepo stores one repo's snapshot and child rows in a single transaction.
func writeRepo(ctx context.Context, db *pgxpool.Pool, r *repoResult) error {
	breakdown, err := json.Marshal(r.Health.Checks)
	if err != nil {
		return err
	}
	g := r.Repo
	topics := g.Topics
	if topics == nil {
		topics = []string{}
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO repo_snapshots (scan_id, repo_id, name, description, visibility, is_fork, is_archived,
			  stars, forks, size_kb, default_branch, topics, license_spdx, created_at, pushed_at,
			  has_readme, has_license, has_gitignore, has_ci, has_tests, open_prs, open_issues,
			  health_score, health_breakdown, status)
			VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,NULLIF($13,''),$14,$15,
			  $16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`,
			r.ScanID, g.ID, g.Ref.Name, g.Description, g.Visibility, g.IsFork, g.IsArchived,
			g.Stars, g.Forks, g.SizeKB, g.DefaultBranch, topics, g.LicenseSPDX, g.CreatedAt, g.PushedAt,
			r.Files.HasReadme, r.Files.HasLicense, r.Files.HasGitignore, r.Files.HasCI, r.Files.HasTests,
			g.OpenPRCount, g.OpenIssueCount, r.Health.Score, breakdown, string(r.Health.Status))
		if err != nil {
			return err
		}

		var rows [][]any
		for _, l := range g.Languages {
			rows = append(rows, []any{r.ScanID, g.ID, l.Language, l.Bytes})
		}
		if err := copyRows(ctx, tx, "repo_languages", []string{"scan_id", "repo_id", "language", "bytes"}, rows); err != nil {
			return err
		}

		rows = rows[:0]
		for _, w := range r.Weeks {
			days := make([]int16, 7)
			for i, d := range w.Days {
				days[i] = int16(d)
			}
			rows = append(rows, []any{r.ScanID, g.ID, w.WeekStart, w.Total, days})
		}
		if err := copyRows(ctx, tx, "repo_commit_weeks", []string{"scan_id", "repo_id", "week_start", "total", "days"}, rows); err != nil {
			return err
		}

		rows = rows[:0]
		for _, b := range g.Branches {
			rows = append(rows, []any{r.ScanID, g.ID, b.Name, b.LastCommitAt, b.Name == g.DefaultBranch})
		}
		if err := copyRows(ctx, tx, "repo_branches", []string{"scan_id", "repo_id", "name", "last_commit_at", "is_default"}, rows); err != nil {
			return err
		}

		rows = rows[:0]
		itemCols := []string{"scan_id", "repo_id", "kind", "number", "title", "author", "url", "is_draft",
			"review_requested_from_me", "assigned_to_me", "created_at", "updated_at"}
		for _, it := range g.OpenPRs {
			rows = append(rows, []any{r.ScanID, g.ID, "pr", it.Number, it.Title, nullStr(it.Author), it.URL, it.IsDraft,
				contains(it.ReviewRequests, r.Login), contains(it.Assignees, r.Login), it.CreatedAt, it.UpdatedAt})
		}
		for _, it := range g.OpenIssues {
			rows = append(rows, []any{r.ScanID, g.ID, "issue", it.Number, it.Title, nullStr(it.Author), it.URL, false,
				false, contains(it.Assignees, r.Login), it.CreatedAt, it.UpdatedAt})
		}
		if err := copyRows(ctx, tx, "repo_items", itemCols, rows); err != nil {
			return err
		}

		rows = rows[:0]
		for _, t := range r.Tech {
			rows = append(rows, []any{r.ScanID, g.ID, t.Name, t.Category, t.Source})
		}
		if err := copyRows(ctx, tx, "repo_tech", []string{"scan_id", "repo_id", "tech", "category", "source"}, rows); err != nil {
			return err
		}

		rows = rows[:0]
		seenRun := map[int64]bool{}
		for _, run := range r.Runs {
			if seenRun[run.ID] {
				continue
			}
			seenRun[run.ID] = true
			rows = append(rows, []any{r.ScanID, g.ID, run.ID, run.Name, nullStr(run.Branch), run.Conclusion, run.URL, run.CreatedAt})
		}
		if err := copyRows(ctx, tx, "workflow_runs", []string{"scan_id", "repo_id", "run_id", "workflow", "branch", "conclusion", "url", "created_at"}, rows); err != nil {
			return err
		}

		if len(r.Commits) > 0 {
			shas := make([]string, len(r.Commits))
			at := make([]time.Time, len(r.Commits))
			add := make([]int32, len(r.Commits))
			del := make([]int32, len(r.Commits))
			for i, c := range r.Commits {
				shas[i], at[i], add[i], del[i] = c.SHA, c.AuthoredAt, int32(c.Additions), int32(c.Deletions)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO commits (repo_id, sha, user_id, authored_at, additions, deletions)
				SELECT $1, s, $2, a, ad, de FROM unnest($3::text[], $4::timestamptz[], $5::int[], $6::int[]) AS t(s, a, ad, de)
				ON CONFLICT (repo_id, sha) DO NOTHING`, g.ID, r.UserID, shas, at, add, del); err != nil {
				return err
			}
		}

		for kind, actors := range map[string][]github.Actor{"fork": g.RecentForks} {
			for _, a := range actors {
				if a.Login == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO repo_stargazers (repo_id, kind, actor, at, first_seen_scan_id) VALUES ($1,$2,$3,$4,$5)
					ON CONFLICT (repo_id, kind, actor) DO NOTHING`, g.ID, kind, a.Login, a.At, r.ScanID); err != nil {
					return err
				}
			}
		}

		ts := r.TrendSnap
		if _, err := tx.Exec(ctx, `
			INSERT INTO repo_trend_snapshots (
			  repo_id, scan_id, scanned_at, health_score, open_prs, open_issues,
			  stale_branches, failed_runs, commit_count_7d, commit_count_30d,
			  unique_authors_30d, top_author_login, top_author_pct, stars, forks
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15)
			ON CONFLICT (repo_id, scan_id) DO UPDATE SET
			  scanned_at = EXCLUDED.scanned_at, health_score = EXCLUDED.health_score,
			  open_prs = EXCLUDED.open_prs, open_issues = EXCLUDED.open_issues,
			  stale_branches = EXCLUDED.stale_branches, failed_runs = EXCLUDED.failed_runs,
			  commit_count_7d = EXCLUDED.commit_count_7d, commit_count_30d = EXCLUDED.commit_count_30d,
			  unique_authors_30d = EXCLUDED.unique_authors_30d, top_author_login = EXCLUDED.top_author_login,
			  top_author_pct = EXCLUDED.top_author_pct, stars = EXCLUDED.stars, forks = EXCLUDED.forks`,
			ts.RepoID, ts.ScanID, ts.ScannedAt, ts.HealthScore, ts.OpenPRs, ts.OpenIssues,
			ts.StaleBranches, ts.FailedRuns, ts.CommitCount7d, ts.CommitCount30d,
			ts.UniqueAuthors30d, ts.TopAuthorLogin, ts.TopAuthorPct, ts.Stars, ts.Forks); err != nil {
			return err
		}
		return nil
	})
}

func loadBaseline(ctx context.Context, db *pgxpool.Pool, repoID, scanID int64, now time.Time) (trends.Baseline, error) {
	var b trends.Baseline
	querySnap := func(where string, args ...any) (*trends.Snapshot, error) {
		var s trends.Snapshot
		err := db.QueryRow(ctx, `
			SELECT repo_id, scan_id, scanned_at, health_score, open_prs, open_issues,
			  stale_branches, failed_runs, commit_count_7d, commit_count_30d,
			  unique_authors_30d, coalesce(top_author_login,''), top_author_pct, stars, forks
			FROM repo_trend_snapshots `+where+` LIMIT 1`, args...).Scan(
			&s.RepoID, &s.ScanID, &s.ScannedAt, &s.HealthScore, &s.OpenPRs, &s.OpenIssues,
			&s.StaleBranches, &s.FailedRuns, &s.CommitCount7d, &s.CommitCount30d,
			&s.UniqueAuthors30d, &s.TopAuthorLogin, &s.TopAuthorPct, &s.Stars, &s.Forks)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &s, err
	}
	var err error
	if b.PrevScan, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 ORDER BY scan_id DESC`, repoID, scanID); err != nil {
		return b, err
	}
	if b.Days7, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 AND scanned_at <= $3 ORDER BY scanned_at DESC`, repoID, scanID, now.Add(-7*24*time.Hour)); err != nil {
		return b, err
	}
	if b.Days30, err = querySnap(`WHERE repo_id = $1 AND scan_id < $2 AND scanned_at <= $3 ORDER BY scanned_at DESC`, repoID, scanID, now.Add(-30*24*time.Hour)); err != nil {
		return b, err
	}
	return b, nil
}

func evaluateTrends(ctx context.Context, db *pgxpool.Pool, userID, scanID int64, now time.Time, results []*repoResult) error {
	for _, r := range results {
		if r == nil {
			continue
		}
		base, err := loadBaseline(ctx, db, r.Repo.ID, scanID, now)
		if err != nil {
			return err
		}
		res := trends.Evaluate(r.TrendSnap, base, r.TrendCtx, now)
		activeRules := make([]string, 0, len(res.Alerts))
		for _, a := range res.Alerts {
			activeRules = append(activeRules, a.Rule)
			detailJSON, err := json.Marshal(a.Detail)
			if err != nil {
				detailJSON = []byte("{}")
			}
			if _, err := db.Exec(ctx, `
				INSERT INTO action_items (user_id, repo_id, rule, severity, title, detail, url, active, last_scan_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,true,$8)
				ON CONFLICT (user_id, repo_id, rule) DO UPDATE SET
				  severity = EXCLUDED.severity,
				  title = EXCLUDED.title,
				  detail = EXCLUDED.detail,
				  url = EXCLUDED.url,
				  active = true,
				  last_scan_id = EXCLUDED.last_scan_id,
				  updated_at = now()`,
				userID, r.Repo.ID, a.Rule, a.Severity, a.Title, detailJSON, a.URL, scanID); err != nil {
				return err
			}
		}
		if _, err := db.Exec(ctx, `
			UPDATE action_items
			SET active = false, last_scan_id = $3, updated_at = now()
			WHERE user_id = $1 AND repo_id = $2 AND active = true AND NOT (rule = ANY($4))`,
			userID, r.Repo.ID, scanID, activeRules); err != nil {
			return err
		}
	}
	return nil
}

func copyRows(ctx context.Context, tx pgx.Tx, table string, cols []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(rows))
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// prune keeps detail rows for the latest keepDetail succeeded scans (summary
// snapshots and languages are kept forever for trends) and drops failed scans
// older than 30 days.
func prune(ctx context.Context, db *pgxpool.Pool, userID int64, keepDetail int) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, table := range []string{"repo_commit_weeks", "repo_branches", "repo_items", "repo_tech", "workflow_runs"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE scan_id IN (
				SELECT id FROM scans WHERE user_id = $1 AND status = 'succeeded' ORDER BY id DESC OFFSET $2)`,
				userID, keepDetail); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM scans WHERE user_id = $1 AND status = 'failed' AND started_at < now() - interval '30 days'`, userID)
		return err
	})
}

// dbCache persists ETag'd REST responses per user.
type dbCache struct {
	db     *pgxpool.Pool
	userID int64
}

const maxCachedBody = 1 << 20

func (c dbCache) Get(ctx context.Context, key string) (string, []byte, bool) {
	var etag string
	var body []byte
	err := c.db.QueryRow(ctx, `SELECT etag, body FROM http_cache WHERE user_id = $1 AND url = $2`, c.userID, key).Scan(&etag, &body)
	return etag, body, err == nil
}

func (c dbCache) Put(ctx context.Context, key, etag string, body []byte) {
	if len(body) > maxCachedBody {
		return
	}
	_, _ = c.db.Exec(ctx, `
		INSERT INTO http_cache (user_id, url, etag, body) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, url) DO UPDATE SET etag = EXCLUDED.etag, body = EXCLUDED.body, fetched_at = now()`,
		c.userID, key, etag, body)
}
