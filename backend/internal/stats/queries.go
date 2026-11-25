package stats

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/trends"
)

var (
	ErrNoData   = errors.New("no completed scan yet")
	ErrNotFound = errors.New("not found")
)

type Service struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, now: time.Now} }

type ScanRef struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

// scans returns the latest and previous succeeded scans.
func (s *Service) scans(ctx context.Context, userID int64) (latest, prev *ScanRef, err error) {
	rows, err := s.db.Query(ctx, `SELECT id, started_at, finished_at FROM scans
		WHERE user_id = $1 AND status = 'succeeded' ORDER BY id DESC LIMIT 2`, userID)
	if err != nil {
		return nil, nil, err
	}
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ScanRef, error) {
		var x ScanRef
		return x, r.Scan(&x.ID, &x.StartedAt, &x.FinishedAt)
	})
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 {
		return nil, nil, ErrNoData
	}
	latest = &refs[0]
	if len(refs) > 1 {
		prev = &refs[1]
	}
	return latest, prev, nil
}

// ---- settings ----

func (s *Service) Settings(ctx context.Context, userID int64) (Settings, error) {
	st := Settings{ExcludedRepoIDs: []int64{}, ExcludedRepoGlobs: []string{}, Timezone: "UTC"}
	err := s.db.QueryRow(ctx, `SELECT excluded_repo_ids, excluded_repo_globs, include_forks, include_archived, timezone
		FROM user_settings WHERE user_id = $1`, userID).
		Scan(&st.ExcludedRepoIDs, &st.ExcludedRepoGlobs, &st.IncludeForks, &st.IncludeArchived, &st.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	return st, err
}

// ValidationError is returned for bad user input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func (s *Service) SaveSettings(ctx context.Context, userID int64, st Settings) (Settings, error) {
	if st.Timezone == "" {
		st.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(st.Timezone); err != nil {
		return st, &ValidationError{"unknown timezone " + st.Timezone}
	}
	if len(st.ExcludedRepoIDs) > 1000 || len(st.ExcludedRepoGlobs) > 100 {
		return st, &ValidationError{"too many exclusions"}
	}
	if st.ExcludedRepoIDs == nil {
		st.ExcludedRepoIDs = []int64{}
	}
	globs := []string{}
	for _, g := range st.ExcludedRepoGlobs {
		if g == "" {
			continue
		}
		if _, err := path.Match(g, ""); err != nil || len(g) > 200 {
			return st, &ValidationError{fmt.Sprintf("invalid pattern %q", g)}
		}
		globs = append(globs, g)
	}
	st.ExcludedRepoGlobs = globs
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_settings (user_id, excluded_repo_ids, excluded_repo_globs, include_forks, include_archived, timezone)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id) DO UPDATE SET excluded_repo_ids = EXCLUDED.excluded_repo_ids,
		  excluded_repo_globs = EXCLUDED.excluded_repo_globs, include_forks = EXCLUDED.include_forks,
		  include_archived = EXCLUDED.include_archived, timezone = EXCLUDED.timezone`,
		userID, st.ExcludedRepoIDs, st.ExcludedRepoGlobs, st.IncludeForks, st.IncludeArchived, st.Timezone)
	return st, err
}

// ---- repos ----

type RepoRow struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	FullName        string          `json:"fullName"`
	Source          string          `json:"source"`
	Description     string          `json:"description"`
	Visibility      string          `json:"visibility"`
	IsFork          bool            `json:"isFork"`
	IsArchived      bool            `json:"isArchived"`
	Stars           int             `json:"stars"`
	Forks           int             `json:"forks"`
	SizeKB          int             `json:"sizeKb"`
	DefaultBranch   string          `json:"defaultBranch"`
	Topics          []string        `json:"topics"`
	License         string          `json:"license"`
	CreatedAt       time.Time       `json:"createdAt"`
	PushedAt        *time.Time      `json:"pushedAt"`
	HasReadme       bool            `json:"hasReadme"`
	HasLicense      bool            `json:"hasLicense"`
	HasGitignore    bool            `json:"hasGitignore"`
	HasCI           bool            `json:"hasCi"`
	HasTests        bool            `json:"hasTests"`
	OpenPRs         int             `json:"openPrs"`
	OpenIssues      int             `json:"openIssues"`
	HealthScore     int             `json:"healthScore"`
	HealthBreakdown json.RawMessage `json:"healthBreakdown"`
	Status          string          `json:"status"`
	PrimaryLanguage string           `json:"primaryLanguage"`
	Excluded        string           `json:"excluded"` // reason, empty when included
	Direction       trends.Direction `json:"direction,omitempty"`
	TrendDelta7d    *trends.DeltaSet `json:"trendDelta7d,omitempty"`
	TrendDelta30d   *trends.DeltaSet `json:"trendDelta30d,omitempty"`
	CommitCount7d   int              `json:"commitCount7d,omitempty"`
	CommitCount30d  int              `json:"commitCount30d,omitempty"`
	UniqueAuthors30d int             `json:"uniqueAuthors30d,omitempty"`
	TopAuthorLogin  string           `json:"topAuthorLogin,omitempty"`
	TopAuthorPct    float64          `json:"topAuthorPct,omitempty"`
	StaleBranches   int              `json:"staleBranches,omitempty"`
	FailedRuns      int              `json:"failedRuns,omitempty"`
}

const repoRowSQL = `
	SELECT s.repo_id, s.name, r.full_name, r.source, coalesce(s.description,''), s.visibility, s.is_fork, s.is_archived,
	  s.stars, s.forks, s.size_kb, coalesce(s.default_branch,''), s.topics, coalesce(s.license_spdx,''),
	  s.created_at, s.pushed_at, s.has_readme, s.has_license, s.has_gitignore, s.has_ci, s.has_tests,
	  s.open_prs, s.open_issues, s.health_score, s.health_breakdown, s.status,
	  coalesce((SELECT l.language FROM repo_languages l WHERE l.scan_id = s.scan_id AND l.repo_id = s.repo_id
	            ORDER BY l.bytes DESC LIMIT 1), ''),
	  coalesce(ts.commit_count_7d, 0), coalesce(ts.commit_count_30d, 0), coalesce(ts.unique_authors_30d, 0),
	  coalesce(ts.top_author_login, ''), coalesce(ts.top_author_pct, 0), coalesce(ts.stale_branches, 0), coalesce(ts.failed_runs, 0)
	FROM repo_snapshots s
	JOIN repos r ON r.id = s.repo_id
	LEFT JOIN repo_trend_snapshots ts ON ts.scan_id = s.scan_id AND ts.repo_id = s.repo_id
	WHERE s.scan_id = $1`

func scanRepoRow(row pgx.Row, st Settings) (RepoRow, error) {
	var r RepoRow
	err := row.Scan(&r.ID, &r.Name, &r.FullName, &r.Source, &r.Description, &r.Visibility, &r.IsFork, &r.IsArchived,
		&r.Stars, &r.Forks, &r.SizeKB, &r.DefaultBranch, &r.Topics, &r.License, &r.CreatedAt, &r.PushedAt,
		&r.HasReadme, &r.HasLicense, &r.HasGitignore, &r.HasCI, &r.HasTests, &r.OpenPRs, &r.OpenIssues,
		&r.HealthScore, &r.HealthBreakdown, &r.Status, &r.PrimaryLanguage,
		&r.CommitCount7d, &r.CommitCount30d, &r.UniqueAuthors30d,
		&r.TopAuthorLogin, &r.TopAuthorPct, &r.StaleBranches, &r.FailedRuns)
	r.Excluded = st.Exclusion(r.ID, r.Name, r.IsFork, r.IsArchived)
	return r, err
}

func (s *Service) repoRows(ctx context.Context, scanID int64, st Settings) ([]RepoRow, error) {
	return s.repoRowsForSource(ctx, scanID, st, "")
}

func (s *Service) repoRowsForSource(ctx context.Context, scanID int64, st Settings, source string) ([]RepoRow, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if source == "owned" || source == "watched" {
		rows, err = s.db.Query(ctx, repoRowSQL+` AND r.source = $2 ORDER BY lower(s.name)`, scanID, source)
	} else {
		rows, err = s.db.Query(ctx, repoRowSQL+` ORDER BY lower(s.name)`, scanID)
	}
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RepoRow, error) { return scanRepoRow(r, st) })
}

type ReposResponse struct {
	Scan  ScanRef   `json:"scan"`
	Repos []RepoRow `json:"repos"`
}

func (s *Service) Repos(ctx context.Context, userID int64) (*ReposResponse, error) {
	return s.ReposForSource(ctx, userID, "")
}

func (s *Service) ReposForSource(ctx context.Context, userID int64, source string) (*ReposResponse, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	repos, err := s.repoRowsForSource(ctx, latest.ID, st, source)
	if err != nil {
		return nil, err
	}
	repoTrends, err := s.trendsForRepos(ctx, userID, latest.ID, latest.StartedAt, repos)
	if err == nil {
		byID := make(map[int64]RepoTrend, len(repoTrends))
		for _, rt := range repoTrends {
			byID[rt.ID] = rt
		}
		for i := range repos {
			if rt, ok := byID[repos[i].ID]; ok {
				repos[i].Direction = rt.Direction
				repos[i].TrendDelta7d = rt.Delta7d
				repos[i].TrendDelta30d = rt.Delta30d
			}
		}
	}
	return &ReposResponse{Scan: *latest, Repos: repos}, nil
}

func includedIDs(repos []RepoRow) (map[int64]bool, []int64) {
	m := map[int64]bool{}
	ids := []int64{}
	for _, r := range repos {
		if r.Excluded == "" {
			m[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	return m, ids
}

// ---- overview ----

type HistoryPoint struct {
	ScanID    int64     `json:"scanId"`
	Date      time.Time `json:"date"`
	Repos     int       `json:"repos"`
	Stars     int       `json:"stars"`
	Forks     int       `json:"forks"`
	AvgHealth float64   `json:"avgHealth"`
}

type Overview struct {
	Scan         ScanRef        `json:"scan"`
	PrevScan     *ScanRef       `json:"prevScan"`
	TotalRepos   int            `json:"totalRepos"`
	Included     int            `json:"includedRepos"`
	Forks        int            `json:"forkRepos"`
	Archived     int            `json:"archivedRepos"`
	Private      int            `json:"privateRepos"`
	Stars        int            `json:"stars"`
	ForkCount    int            `json:"forks"`
	OpenPRs      int            `json:"openPrs"`
	OpenIssues   int            `json:"openIssues"`
	AvgHealth    float64        `json:"avgHealth"`
	StatusCounts map[string]int `json:"statusCounts"`
	TrendSummary map[string]int `json:"trendSummary"`
	History      []HistoryPoint `json:"history"`
}

func (s *Service) Overview(ctx context.Context, userID int64) (*Overview, error) {
	latest, prev, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	repos, err := s.repoRowsForSource(ctx, latest.ID, st, "owned")
	if err != nil {
		return nil, err
	}
	o := &Overview{Scan: *latest, PrevScan: prev, TotalRepos: len(repos),
		StatusCounts: map[string]int{"active": 0, "slowing": 0, "dormant": 0},
		TrendSummary: map[string]int{"improving": 0, "steady": 0, "slipping": 0}}
	healthSum := 0
	for _, r := range repos {
		if r.IsFork {
			o.Forks++
		}
		if r.IsArchived {
			o.Archived++
		}
		if r.Visibility == "private" {
			o.Private++
		}
		if r.Excluded != "" {
			continue
		}
		o.Included++
		o.Stars += r.Stars
		o.ForkCount += r.Forks
		o.OpenPRs += r.OpenPRs
		o.OpenIssues += r.OpenIssues
		o.StatusCounts[r.Status]++
		healthSum += r.HealthScore
	}
	if o.Included > 0 {
		o.AvgHealth = round1(float64(healthSum) / float64(o.Included))
	}
	if repoTrends, err := s.trendsForRepos(ctx, userID, latest.ID, latest.StartedAt, repos); err == nil {
		for _, rt := range repoTrends {
			o.TrendSummary[string(rt.Direction)]++
		}
	}

	// History: one point per succeeded scan, with today's exclusion settings (owned repos only).
	rows, err := s.db.Query(ctx, `
		SELECT sc.id, sc.started_at, s.repo_id, s.name, s.is_fork, s.is_archived, s.stars, s.forks, s.health_score
		FROM scans sc
		JOIN repo_snapshots s ON s.scan_id = sc.id
		JOIN repos r ON r.id = s.repo_id
		WHERE sc.user_id = $1 AND sc.status = 'succeeded' AND r.source = 'owned' ORDER BY sc.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type agg struct {
		HistoryPoint
		healthSum int
	}
	var pts []*agg
	for rows.Next() {
		var (
			id, repoID          int64
			at                  time.Time
			name                string
			fork, archived      bool
			stars, forks, score int
		)
		if err := rows.Scan(&id, &at, &repoID, &name, &fork, &archived, &stars, &forks, &score); err != nil {
			return nil, err
		}
		if len(pts) == 0 || pts[len(pts)-1].ScanID != id {
			pts = append(pts, &agg{HistoryPoint: HistoryPoint{ScanID: id, Date: at}})
		}
		if st.Exclusion(repoID, name, fork, archived) != "" {
			continue
		}
		p := pts[len(pts)-1]
		p.Repos++
		p.Stars += stars
		p.Forks += forks
		p.healthSum += score
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	o.History = make([]HistoryPoint, 0, len(pts))
	for _, p := range pts {
		if p.Repos > 0 {
			p.AvgHealth = round1(float64(p.healthSum) / float64(p.Repos))
		}
		o.History = append(o.History, p.HistoryPoint)
	}
	return o, nil
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// ---- analytics ----

type RepoLanguages struct {
	RepoID    int64           `json:"repoId"`
	Name      string          `json:"name"`
	Languages []LanguageShare `json:"languages"`
}

type TopRepo struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type TechUse struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Repos    []string `json:"repos"`
}

type Analytics struct {
	Scan           ScanRef         `json:"scan"`
	Timezone       string          `json:"timezone"`
	IncludedRepos  int             `json:"includedRepos"`
	Languages      []LanguageShare `json:"languages"`
	PerRepo        []RepoLanguages `json:"perRepo"`
	Trend          []YearShare     `json:"trend"`
	TrendLanguages []string        `json:"trendLanguages"`
	Weekly         []WeekPoint     `json:"weekly"`
	Commits        CommitStats     `json:"commits"`
	TopByStars     []TopRepo       `json:"topByStars"`
	TopByForks     []TopRepo       `json:"topByForks"`
	TopByCommits   []TopRepo       `json:"topByCommits"`
	Tech           []TechUse       `json:"tech"`
}

const trendLanguages = 8

func (s *Service) Analytics(ctx context.Context, userID int64) (*Analytics, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		loc = time.UTC
	}
	repos, err := s.repoRowsForSource(ctx, latest.ID, st, "owned")
	if err != nil {
		return nil, err
	}
	included, ids := includedIDs(repos)
	a := &Analytics{Scan: *latest, Timezone: loc.String(), IncludedRepos: len(ids)}

	// Languages.
	rows, err := s.db.Query(ctx, `SELECT repo_id, language, bytes FROM repo_languages WHERE scan_id = $1`, latest.ID)
	if err != nil {
		return nil, err
	}
	langs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RepoLanguage, error) {
		var x RepoLanguage
		return x, r.Scan(&x.RepoID, &x.Language, &x.Bytes)
	})
	if err != nil {
		return nil, err
	}
	a.Languages = Languages(langs, included)
	a.TrendLanguages = TopLanguages(a.Languages, trendLanguages)

	byRepo := map[int64][]RepoLanguage{}
	for _, l := range langs {
		byRepo[l.RepoID] = append(byRepo[l.RepoID], l)
	}
	spans := make([]RepoSpan, 0, len(repos))
	a.PerRepo = []RepoLanguages{}
	for _, r := range repos {
		spans = append(spans, RepoSpan{RepoID: r.ID, CreatedAt: r.CreatedAt, PushedAt: r.PushedAt})
		if included[r.ID] && len(byRepo[r.ID]) > 0 {
			a.PerRepo = append(a.PerRepo, RepoLanguages{RepoID: r.ID, Name: r.Name,
				Languages: Languages(byRepo[r.ID], map[int64]bool{r.ID: true})})
		}
	}
	slices.SortFunc(a.PerRepo, func(x, y RepoLanguages) int {
		return cmp.Compare(totalBytes(y.Languages), totalBytes(x.Languages))
	})
	a.Trend = LanguageTrend(spans, langs, included, a.TrendLanguages)

	// Commits.
	now := s.now()
	all := map[string]int{}
	rows, err = s.db.Query(ctx, `SELECT week_start, sum(total)::int FROM repo_commit_weeks
		WHERE scan_id = $1 AND repo_id = ANY($2) GROUP BY week_start`, latest.ID, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var w time.Time
		var n int
		if err := rows.Scan(&w, &n); err != nil {
			rows.Close()
			return nil, err
		}
		all[w.Format(time.DateOnly)] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(ctx, `SELECT repo_id, authored_at FROM commits
		WHERE user_id = $1 AND repo_id = ANY($2) AND authored_at > $3`, userID, ids, now.AddDate(-1, 0, 0))
	if err != nil {
		return nil, err
	}
	var mine []time.Time
	perRepo := map[int64]int{}
	for rows.Next() {
		var id int64
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return nil, err
		}
		mine = append(mine, t)
		perRepo[id]++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	a.Weekly = Weekly(all, mine, now, 52)
	a.Commits = Commits(mine, loc, now)

	// Top repos.
	var inc []RepoRow
	for _, r := range repos {
		if included[r.ID] {
			inc = append(inc, r)
		}
	}
	a.TopByStars = top(inc, func(r RepoRow) int { return r.Stars })
	a.TopByForks = top(inc, func(r RepoRow) int { return r.Forks })
	a.TopByCommits = top(inc, func(r RepoRow) int { return perRepo[r.ID] })

	// Tech stack.
	rows, err = s.db.Query(ctx, `SELECT t.tech, t.category, s.name FROM repo_tech t
		JOIN repo_snapshots s ON s.scan_id = t.scan_id AND s.repo_id = t.repo_id
		WHERE t.scan_id = $1 AND t.repo_id = ANY($2) ORDER BY t.tech, s.name`, latest.ID, ids)
	if err != nil {
		return nil, err
	}
	techIdx := map[string]int{}
	a.Tech = []TechUse{}
	for rows.Next() {
		var name, cat, repo string
		if err := rows.Scan(&name, &cat, &repo); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := techIdx[name]
		if !ok {
			i = len(a.Tech)
			techIdx[name] = i
			a.Tech = append(a.Tech, TechUse{Name: name, Category: cat})
		}
		a.Tech[i].Repos = append(a.Tech[i].Repos, repo)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortStableFunc(a.Tech, func(x, y TechUse) int { return cmp.Compare(len(y.Repos), len(x.Repos)) })
	return a, nil
}

func totalBytes(ls []LanguageShare) int64 {
	var t int64
	for _, l := range ls {
		t += l.Bytes
	}
	return t
}

// top returns up to 10 repos with a positive value, highest first.
func top(repos []RepoRow, val func(RepoRow) int) []TopRepo {
	out := []TopRepo{}
	for _, r := range repos {
		if v := val(r); v > 0 {
			out = append(out, TopRepo{ID: r.ID, Name: r.Name, Value: v})
		}
	}
	slices.SortStableFunc(out, func(a, b TopRepo) int { return cmp.Compare(b.Value, a.Value) })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// ---- repo detail ----

type BranchInfo struct {
	Name         string     `json:"name"`
	LastCommitAt *time.Time `json:"lastCommitAt"`
	IsDefault    bool       `json:"isDefault"`
	Stale        bool       `json:"stale"`
}

type ItemInfo struct {
	Kind            string    `json:"kind"`
	Number          int       `json:"number"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	URL             string    `json:"url"`
	IsDraft         bool      `json:"isDraft"`
	ReviewRequested bool      `json:"reviewRequested"`
	AssignedToMe    bool      `json:"assignedToMe"`
	CreatedAt       time.Time `json:"createdAt"`
	AgeDays         int       `json:"ageDays"`
	Repo            string    `json:"repo,omitempty"`
}

type RunInfo struct {
	Repo      string    `json:"repo,omitempty"`
	Workflow  string    `json:"workflow"`
	Branch    string    `json:"branch"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

type RepoHistoryPoint struct {
	Date   time.Time `json:"date"`
	Health int       `json:"health"`
	Stars  int       `json:"stars"`
	Forks  int       `json:"forks"`
}

type RepoDetail struct {
	Scan      ScanRef            `json:"scan"`
	Repo      RepoRow            `json:"repo"`
	Languages []LanguageShare    `json:"languages"`
	Branches  []BranchInfo       `json:"branches"`
	Items     []ItemInfo         `json:"items"`
	Tech      []TechUse          `json:"tech"`
	Runs      []RunInfo          `json:"failedRuns"`
	History   []RepoHistoryPoint `json:"history"`
	Weekly    []WeekPoint        `json:"weekly"`
	Trend     *RepoTrendDetail   `json:"trend,omitempty"`
}

func (s *Service) Repo(ctx context.Context, userID, repoID int64) (*RepoDetail, error) {
	latest, _, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	// The repos.user_id check scopes the lookup to the caller.
	repo, err := scanRepoRow(s.db.QueryRow(ctx, repoRowSQL+` AND s.repo_id = $2 AND r.user_id = $3`, latest.ID, repoID, userID), st)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	d := &RepoDetail{Scan: *latest, Repo: repo, Branches: []BranchInfo{}, Items: []ItemInfo{}, Tech: []TechUse{},
		Runs: []RunInfo{}, History: []RepoHistoryPoint{}}

	rows, _ := s.db.Query(ctx, `SELECT repo_id, language, bytes FROM repo_languages WHERE scan_id = $1 AND repo_id = $2`, latest.ID, repoID)
	langs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RepoLanguage, error) {
		var x RepoLanguage
		return x, r.Scan(&x.RepoID, &x.Language, &x.Bytes)
	})
	if err != nil {
		return nil, err
	}
	d.Languages = Languages(langs, map[int64]bool{repoID: true})

	rows, _ = s.db.Query(ctx, `SELECT name, last_commit_at, is_default FROM repo_branches
		WHERE scan_id = $1 AND repo_id = $2 ORDER BY is_default DESC, last_commit_at DESC NULLS LAST`, latest.ID, repoID)
	d.Branches, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (BranchInfo, error) {
		var b BranchInfo
		err := r.Scan(&b.Name, &b.LastCommitAt, &b.IsDefault)
		b.Stale = !b.IsDefault && b.LastCommitAt != nil && now.Sub(*b.LastCommitAt) > 90*24*time.Hour
		return b, err
	})
	if err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `SELECT kind, number, title, coalesce(author,''), url, is_draft,
		review_requested_from_me, assigned_to_me, created_at, '' FROM repo_items
		WHERE scan_id = $1 AND repo_id = $2 ORDER BY kind DESC, created_at`, latest.ID, repoID)
	if d.Items, err = collectItems(rows, now); err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `SELECT tech, category, source FROM repo_tech WHERE scan_id = $1 AND repo_id = $2 ORDER BY category, tech`, latest.ID, repoID)
	d.Tech, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TechUse, error) {
		var t TechUse
		var src string
		err := r.Scan(&t.Name, &t.Category, &src)
		t.Repos = []string{src}
		return t, err
	})
	if err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `SELECT '', workflow, coalesce(branch,''), url, created_at FROM workflow_runs
		WHERE scan_id = $1 AND repo_id = $2 ORDER BY created_at DESC`, latest.ID, repoID)
	if d.Runs, err = collectRuns(rows); err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `SELECT sc.started_at, s.health_score, s.stars, s.forks FROM repo_snapshots s
		JOIN scans sc ON sc.id = s.scan_id WHERE s.repo_id = $1 AND sc.user_id = $2 AND sc.status = 'succeeded'
		ORDER BY sc.id`, repoID, userID)
	d.History, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (RepoHistoryPoint, error) {
		var p RepoHistoryPoint
		return p, r.Scan(&p.Date, &p.Health, &p.Stars, &p.Forks)
	})
	if err != nil {
		return nil, err
	}

	all := map[string]int{}
	rows, _ = s.db.Query(ctx, `SELECT week_start, total FROM repo_commit_weeks WHERE scan_id = $1 AND repo_id = $2`, latest.ID, repoID)
	if _, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var w time.Time
		var n int
		err := r.Scan(&w, &n)
		all[w.Format(time.DateOnly)] = n
		return struct{}{}, err
	}); err != nil {
		return nil, err
	}
	rows, _ = s.db.Query(ctx, `SELECT authored_at FROM commits WHERE repo_id = $1 AND user_id = $2 AND authored_at > $3`,
		repoID, userID, now.AddDate(-1, 0, 0))
	mine, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return nil, err
	}
	d.Weekly = Weekly(all, mine, now, 52)
	if td, err := s.TrendForRepo(ctx, userID, repoID); err == nil {
		d.Trend = td
	}
	return d, nil
}

func collectItems(rows pgx.Rows, now time.Time) ([]ItemInfo, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemInfo, error) {
		var it ItemInfo
		err := r.Scan(&it.Kind, &it.Number, &it.Title, &it.Author, &it.URL, &it.IsDraft,
			&it.ReviewRequested, &it.AssignedToMe, &it.CreatedAt, &it.Repo)
		it.AgeDays = int(now.Sub(it.CreatedAt).Hours() / 24)
		return it, err
	})
}

func collectRuns(rows pgx.Rows) ([]RunInfo, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RunInfo, error) {
		var x RunInfo
		return x, r.Scan(&x.Repo, &x.Workflow, &x.Branch, &x.URL, &x.CreatedAt)
	})
}

// ---- digest ----

type ActorEvent struct {
	Repo  string    `json:"repo"`
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
}

type CountDelta struct {
	Repo  string `json:"repo"`
	Stars int    `json:"stars"`
	Forks int    `json:"forks"`
}

type Digest struct {
	Scan          ScanRef      `json:"scan"`
	Since         time.Time    `json:"since"`
	SincePrevScan bool         `json:"sincePrevScan"`
	WaitingOnMe   []ItemInfo   `json:"waitingOnMe"`
	NewIssues     []ItemInfo   `json:"newIssues"`
	FailedRuns    []RunInfo    `json:"failedRuns"`
	NewStars      []ActorEvent `json:"newStars"`
	NewForks      []ActorEvent `json:"newForks"`
	Deltas        []CountDelta `json:"deltas"`
	WhatToFixNext []ActionItem `json:"whatToFixNext"`
}

// Digest summarises what changed since the previous scan (or the last 7 days
// when there is only one scan).
func (s *Service) Digest(ctx context.Context, userID int64, login string) (*Digest, error) {
	latest, prev, err := s.scans(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	repos, err := s.repoRowsForSource(ctx, latest.ID, st, "owned")
	if err != nil {
		return nil, err
	}
	_, ids := includedIDs(repos)
	now := s.now()
	d := &Digest{Scan: *latest, Since: latest.StartedAt.AddDate(0, 0, -7), NewStars: []ActorEvent{}, NewForks: []ActorEvent{}, Deltas: []CountDelta{}}
	if prev != nil {
		d.Since, d.SincePrevScan = prev.StartedAt, true
	}

	// Open PRs on my repos that need me: review requested, or opened by
	// someone else and not a draft.
	rows, _ := s.db.Query(ctx, `
		SELECT i.kind, i.number, i.title, coalesce(i.author,''), i.url, i.is_draft, i.review_requested_from_me,
		  i.assigned_to_me, i.created_at, s.name
		FROM repo_items i JOIN repo_snapshots s ON s.scan_id = i.scan_id AND s.repo_id = i.repo_id
		WHERE i.scan_id = $1 AND i.repo_id = ANY($2) AND i.kind = 'pr'
		  AND (i.review_requested_from_me OR (NOT i.is_draft AND i.author IS DISTINCT FROM $3))
		ORDER BY i.created_at`, latest.ID, ids, login)
	if d.WaitingOnMe, err = collectItems(rows, now); err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `
		SELECT i.kind, i.number, i.title, coalesce(i.author,''), i.url, i.is_draft, i.review_requested_from_me,
		  i.assigned_to_me, i.created_at, s.name
		FROM repo_items i JOIN repo_snapshots s ON s.scan_id = i.scan_id AND s.repo_id = i.repo_id
		WHERE i.scan_id = $1 AND i.repo_id = ANY($2) AND i.kind = 'issue' AND i.created_at > $3
		ORDER BY i.created_at DESC`, latest.ID, ids, d.Since)
	if d.NewIssues, err = collectItems(rows, now); err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `
		SELECT s.name, w.workflow, coalesce(w.branch,''), w.url, w.created_at
		FROM workflow_runs w JOIN repo_snapshots s ON s.scan_id = w.scan_id AND s.repo_id = w.repo_id
		WHERE w.scan_id = $1 AND w.repo_id = ANY($2) AND w.created_at > $3
		ORDER BY w.created_at DESC`, latest.ID, ids, d.Since)
	if d.FailedRuns, err = collectRuns(rows); err != nil {
		return nil, err
	}

	rows, _ = s.db.Query(ctx, `
		SELECT g.kind, r.full_name, g.actor, g.at FROM repo_stargazers g JOIN repos r ON r.id = g.repo_id
		WHERE g.repo_id = ANY($1) AND g.at > $2 ORDER BY g.at DESC`, ids, d.Since)
	if _, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var kind string
		var e ActorEvent
		err := r.Scan(&kind, &e.Repo, &e.Actor, &e.At)
		if kind == "star" {
			d.NewStars = append(d.NewStars, e)
		} else {
			d.NewForks = append(d.NewForks, e)
		}
		return struct{}{}, err
	}); err != nil {
		return nil, err
	}

	if prev != nil {
		rows, _ = s.db.Query(ctx, `
			SELECT l.name, l.stars - p.stars, l.forks - p.forks
			FROM repo_snapshots l JOIN repo_snapshots p ON p.repo_id = l.repo_id AND p.scan_id = $2
			WHERE l.scan_id = $1 AND l.repo_id = ANY($3) AND (l.stars <> p.stars OR l.forks <> p.forks)
			ORDER BY l.stars - p.stars DESC`, latest.ID, prev.ID, ids)
		if d.Deltas, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (CountDelta, error) {
			var c CountDelta
			return c, r.Scan(&c.Repo, &c.Stars, &c.Forks)
		}); err != nil {
			return nil, err
		}
	}
	if top5, err := s.ActionItems(ctx, userID, 5); err == nil {
		d.WhatToFixNext = top5
	} else {
		d.WhatToFixNext = []ActionItem{}
	}
	return d, nil
}
