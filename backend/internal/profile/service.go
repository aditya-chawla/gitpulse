package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

const commitMessage = "chore(gitpulse): refresh profile README stats"

var (
	ErrNoScan        = errors.New("no successful scan yet")
	ErrRepoNotFound  = errors.New("profile repository not found")
	ErrInvalidConfig = errors.New("invalid profile README settings")
)

type Reader interface {
	Readme(ctx context.Context, repo github.RepoRef) (path, text, sha string, err error)
}

type Writer interface {
	UpdateFile(ctx context.Context, repo github.RepoRef, path, message, content, sha string) (commitSHA, commitURL string, err error)
}

type Access interface {
	Read(ctx context.Context, u *store.User) (Reader, error)
	ReadmeWriter(ctx context.Context, u *store.User, repoID int64) (Writer, error)
}

type Service struct {
	db       *pgxpool.Pool
	stats    *stats.Service
	access   Access
	repoName string // PROFILE_README_REPO; "" = <login>/<login>
	feed     *http.Client
	log      *slog.Logger
}

func New(db *pgxpool.Pool, st *stats.Service, access Access, repoName string, log *slog.Logger) *Service {
	return &Service{db: db, stats: st, access: access, repoName: repoName, feed: NewFeedClient(), log: log}
}

// RepoName is the profile repo for a user.
func (s *Service) RepoName(u *store.User) string {
	if s.repoName != "" {
		return s.repoName
	}
	return u.Login + "/" + u.Login
}

type Settings struct {
	Enabled bool   `json:"enabled"`
	FeedURL string `json:"feedUrl"`
}

func (s *Service) Settings(ctx context.Context, userID int64) (Settings, error) {
	var st Settings
	var feed *string
	err := s.db.QueryRow(ctx, `SELECT profile_readme_enabled, medium_feed_url FROM user_settings WHERE user_id = $1`, userID).Scan(&st.Enabled, &feed)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if feed != nil {
		st.FeedURL = *feed
	}
	return st, err
}

func (s *Service) SaveSettings(ctx context.Context, userID int64, in Settings) (Settings, error) {
	feed, err := ValidateFeedURL(in.FeedURL)
	if err != nil {
		return in, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	in.FeedURL = feed
	_, err = s.db.Exec(ctx, `
		INSERT INTO user_settings (user_id, profile_readme_enabled, medium_feed_url) VALUES ($1,$2,nullif($3,''))
		ON CONFLICT (user_id) DO UPDATE SET profile_readme_enabled = EXCLUDED.profile_readme_enabled, medium_feed_url = EXCLUDED.medium_feed_url`,
		userID, in.Enabled, in.FeedURL)
	return in, err
}

// Data gathers the README content: public repos only, honouring the user's
// exclusions.
func (s *Service) Data(ctx context.Context, u *store.User, profileRepo string) (Data, error) {
	var d Data
	var scanID int64
	err := s.db.QueryRow(ctx, `SELECT id FROM scans WHERE user_id = $1 AND status = 'succeeded' ORDER BY id DESC LIMIT 1`, u.ID).Scan(&scanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNoScan
	}
	if err != nil {
		return d, err
	}
	st, err := s.stats.Settings(ctx, u.ID)
	if err != nil {
		return d, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.full_name, s.name, coalesce(s.description,''), s.stars, s.pushed_at, s.is_fork, s.is_archived
		FROM repo_snapshots s JOIN repos r ON r.id = s.repo_id
		WHERE s.scan_id = $1 AND s.visibility = 'public'`, scanID)
	if err != nil {
		return d, err
	}
	type repo struct {
		id                 int64
		full, name, desc   string
		stars              int
		pushed             *time.Time
		fork, archived, in bool
	}
	var repos []repo
	included := map[int64]bool{}
	for rows.Next() {
		var r repo
		if err := rows.Scan(&r.id, &r.full, &r.name, &r.desc, &r.stars, &r.pushed, &r.fork, &r.archived); err != nil {
			rows.Close()
			return d, err
		}
		r.in = st.Exclusion(r.id, r.name, r.fork, r.archived) == ""
		if r.in {
			included[r.id] = true
		}
		repos = append(repos, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}

	// Projects: public, included, non-fork, non-archived, not the profile repo.
	slices.SortFunc(repos, func(a, b repo) int {
		switch {
		case a.pushed == nil && b.pushed == nil:
			return 0
		case a.pushed == nil:
			return 1
		case b.pushed == nil:
			return -1
		}
		return b.pushed.Compare(*a.pushed)
	})
	for _, r := range repos {
		if !r.in || r.fork || r.archived || strings.EqualFold(r.full, profileRepo) || r.pushed == nil {
			continue
		}
		d.Projects = append(d.Projects, Project{Name: r.name, URL: "https://github.com/" + r.full, Description: r.desc, Stars: r.stars, PushedAt: *r.pushed})
		if len(d.Projects) == 5 {
			break
		}
	}

	ids := make([]int64, 0, len(included))
	for _, r := range repos {
		if included[r.id] && !r.fork {
			ids = append(ids, r.id)
		}
	}
	lrows, err := s.db.Query(ctx, `
		SELECT language, sum(bytes)::bigint FROM repo_languages
		WHERE scan_id = $1 AND repo_id = ANY($2) GROUP BY language ORDER BY 2 DESC, 1`, scanID, ids)
	if err != nil {
		return d, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l Language
		if err := lrows.Scan(&l.Name, &l.Bytes); err != nil {
			return d, err
		}
		d.Languages = append(d.Languages, l)
	}
	return d, lrows.Err()
}

type Preview struct {
	Repo        string `json:"repo"`
	Enabled     bool   `json:"enabled"`
	FeedURL     string `json:"feedUrl"`
	ReadmePath  string `json:"readmePath,omitempty"`
	MarkerError string `json:"markerError,omitempty"` // "" = markers OK
	Current     string `json:"current"`
	Proposed    string `json:"proposed"`
	Changed     bool   `json:"changed"`
	FeedError   string `json:"feedError,omitempty"`
	Articles    int    `json:"articles"`
	LastRun     *Run   `json:"lastRun,omitempty"`
}

type Run struct {
	Outcome   string    `json:"outcome"` // updated | unchanged | no_markers | disabled | failed
	CommitURL string    `json:"commitUrl,omitempty"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
	commitSHA string
}

type state struct {
	repoID           int64
	ref              github.RepoRef
	path, text, sha  string
	proposed, curBlk string
	markerErr        error
	feedErr          string
	articles         int
}

// prepare reads everything needed for a preview or an update.
func (s *Service) prepare(ctx context.Context, u *store.User, st Settings) (*state, error) {
	name := s.RepoName(u)
	var repoID int64
	// Only a repo the user owns (seen by a scan) can ever be written.
	err := s.db.QueryRow(ctx, `SELECT id FROM repos WHERE user_id = $1 AND lower(full_name) = lower($2)`, u.ID, name).Scan(&repoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s isn't among your scanned repositories (create it, then run a scan)", ErrRepoNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	owner, repoName, _ := strings.Cut(name, "/")
	x := &state{repoID: repoID, ref: github.RepoRef{Owner: owner, Name: repoName}}

	d, err := s.Data(ctx, u, name)
	if err != nil {
		return nil, err
	}
	if st.FeedURL != "" {
		arts, ferr := FetchArticles(ctx, s.feed, st.FeedURL)
		if ferr != nil {
			x.feedErr = ferr.Error()
			s.log.Warn("medium feed", "err", ferr)
		}
		d.Articles = arts
		x.articles = len(arts)
	}
	x.proposed = Render(d)

	rd, err := s.access.Read(ctx, u)
	if err != nil {
		return nil, err
	}
	x.path, x.text, x.sha, err = rd.Readme(ctx, x.ref)
	if err != nil {
		if github.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s has no README", ErrRepoNotFound, name)
		}
		return nil, err
	}
	x.curBlk, x.markerErr = Current(x.text)
	return x, nil
}

func (s *Service) Preview(ctx context.Context, u *store.User) (*Preview, error) {
	st, err := s.Settings(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	x, err := s.prepare(ctx, u, st)
	if err != nil {
		return nil, err
	}
	p := &Preview{Repo: s.RepoName(u), Enabled: st.Enabled, FeedURL: st.FeedURL, ReadmePath: x.path,
		Current: strings.TrimSpace(x.curBlk), Proposed: x.proposed, FeedError: x.feedErr, Articles: x.articles}
	if x.markerErr != nil {
		p.MarkerError = x.markerErr.Error()
	} else {
		p.Changed = !Same(x.curBlk, x.proposed)
	}
	p.LastRun, _ = s.lastRun(ctx, u.ID)
	return p, nil
}

// Apply updates the README if enabled, the markers are valid, and the block
// changed. trigger is "ui" or "cron".
func (s *Service) Apply(ctx context.Context, u *store.User, trigger string) (Run, error) {
	st, err := s.Settings(ctx, u.ID)
	if err != nil {
		return Run{}, err
	}
	name := s.RepoName(u)
	if !st.Enabled {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "disabled"}), nil
	}
	x, err := s.prepare(ctx, u, st)
	if err != nil {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "failed", Error: err.Error()}), err
	}
	if x.markerErr != nil {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "no_markers", Error: x.markerErr.Error()}), nil
	}
	if Same(x.curBlk, x.proposed) {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "unchanged"}), nil
	}
	if x.feedErr != "" && strings.Contains(x.curBlk, "#### Latest articles") {
		// Don't wipe the articles section because Medium was briefly down.
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "failed", Error: "feed unavailable: " + x.feedErr}), nil
	}
	updated, err := Replace(x.text, x.proposed)
	if err != nil {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "no_markers", Error: err.Error()}), nil
	}
	w, err := s.access.ReadmeWriter(ctx, u, x.repoID)
	if err != nil {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "failed", Error: err.Error()}), err
	}
	sha, url, err := w.UpdateFile(ctx, x.ref, x.path, commitMessage, updated, x.sha)
	if err != nil {
		return s.record(ctx, u.ID, name, trigger, Run{Outcome: "failed", Error: err.Error()}), err
	}
	s.log.Info("profile README updated", "repo", name, "commit", sha, "trigger", trigger)
	return s.record(ctx, u.ID, name, trigger, Run{Outcome: "updated", CommitURL: url, commitSHA: sha}), nil
}

func (s *Service) record(ctx context.Context, userID int64, repo, trigger string, r Run) Run {
	r.At = time.Now()
	if _, err := s.db.Exec(ctx, `
		INSERT INTO profile_readme_runs (user_id, repo_name, trigger, outcome, commit_sha, commit_url, error)
		VALUES ($1,$2,$3,$4,nullif($5,''),nullif($6,''),nullif($7,''))`,
		userID, repo, trigger, r.Outcome, r.commitSHA, r.CommitURL, r.Error); err != nil {
		s.log.Error("record profile run", "err", err)
	}
	return r
}

func (s *Service) lastRun(ctx context.Context, userID int64) (*Run, error) {
	var r Run
	var url, e *string
	err := s.db.QueryRow(ctx, `SELECT outcome, commit_url, error, created_at FROM profile_readme_runs
		WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, userID).Scan(&r.Outcome, &url, &e, &r.At)
	if err != nil {
		return nil, err
	}
	if url != nil {
		r.CommitURL = *url
	}
	if e != nil {
		r.Error = *e
	}
	return &r, nil
}
