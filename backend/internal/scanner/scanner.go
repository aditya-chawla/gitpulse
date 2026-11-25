// Package scanner collects a user's repository data from GitHub in the
// background and stores it as a snapshot. The dashboard reads only what the
// scanner wrote.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/detect"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/health"
	"github.com/harshit3011/gitpulse/backend/internal/store"
	"github.com/harshit3011/gitpulse/backend/internal/trends"
)

const (
	treeDepth         = 4
	workers           = 3 // GitHub discourages heavy concurrency (secondary limits)
	scanTimeout       = 45 * time.Minute
	keepDetail        = 30
	commitWindow      = 365 * 24 * time.Hour
	commitRescan      = 7 * 24 * time.Hour  // overlap so late-pushed commits are caught
	runsWindow        = 14 * 24 * time.Hour // failed workflow runs kept for the digest
	maxUnauthWatched  = 10                  // cap unauthenticated external repos per scan
)

var ErrUnknownUser = errors.New("unknown user")

// ClientFactory returns a read-only GitHub client for the user, wired to the
// scanner's ETag cache.
type ClientFactory func(ctx context.Context, u *store.User, cache github.Cache) (github.Client, error)

// Option configures optional Scanner behavior.
type Option func(*Scanner)

// WithPublicToken sets an optional PAT for scanning public watched repos when
// an installation token is not used.
func WithPublicToken(token string) Option {
	return func(s *Scanner) { s.publicToken = token }
}

type Scanner struct {
	db          *pgxpool.Pool
	store       *store.Store
	newClient   ClientFactory
	publicToken string
	log         *slog.Logger
	now         func() time.Time

	baseCtx context.Context
	wg      sync.WaitGroup
}

// New creates a scanner. Scans run under baseCtx, so cancelling it (server
// shutdown) stops them.
func New(baseCtx context.Context, db *pgxpool.Pool, st *store.Store, f ClientFactory, log *slog.Logger, opts ...Option) *Scanner {
	s := &Scanner{db: db, store: st, newClient: f, log: log, now: time.Now, baseCtx: baseCtx}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start begins a background scan for the user. If one is already running its
// ID is returned with existing=true.
func (s *Scanner) Start(ctx context.Context, userID int64, trigger string) (id int64, existing bool, err error) {
	u, err := s.store.GetUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return 0, false, ErrUnknownUser
	}
	if err != nil {
		return 0, false, err
	}
	id, existing, err = insertScan(ctx, s.db, userID, trigger)
	if err != nil || existing {
		return id, existing, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(id, u)
	}()
	return id, false, nil
}

// Wait blocks until running scans exit or the timeout passes.
func (s *Scanner) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func (s *Scanner) run(scanID int64, u *store.User) {
	ctx, cancel := context.WithTimeout(s.baseCtx, scanTimeout)
	defer cancel()
	log := s.log.With("scan_id", scanID, "user", u.Login)
	log.Info("scan started")
	start := time.Now()

	var client github.Client
	warnings, err := func() (warnings []string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		c, err := s.newClient(ctx, u, dbCache{db: s.db, userID: u.ID})
		if err != nil {
			return nil, fmt.Errorf("github access: %w", err)
		}
		client = c
		return s.scan(ctx, scanID, u, client, log)
	}()

	// Finish with a fresh context so the status is recorded even on shutdown.
	fctx, fcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer fcancel()
	var rate github.RateInfo
	if client != nil {
		rate = client.Rate()
	}
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = "interrupted: server shutting down"
		}
		log.Error("scan failed", "err", err, "dur", time.Since(start).Round(time.Second))
		if ferr := finishScan(fctx, s.db, scanID, "failed", msg, rate); ferr != nil {
			log.Error("record scan failure", "err", ferr)
		}
		return
	}
	msg := ""
	if len(warnings) > 0 {
		msg = fmt.Sprintf("%d warning(s): %s", len(warnings), strings.Join(first(warnings, 5), "; "))
	}
	if ferr := finishScan(fctx, s.db, scanID, "succeeded", msg, rate); ferr != nil {
		log.Error("record scan success", "err", ferr)
	}
	if perr := prune(fctx, s.db, u.ID, keepDetail); perr != nil {
		log.Error("prune old scans", "err", perr)
	}
	log.Info("scan finished", "dur", time.Since(start).Round(time.Second), "api_calls", rate.Calls, "warnings", len(warnings))
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// scan does the work. Repo-level problems become warnings; only failures that
// make the whole snapshot meaningless return an error.
func (s *Scanner) scan(ctx context.Context, scanID int64, u *store.User, gh github.Client, log *slog.Logger) ([]string, error) {
	viewer, err := gh.User(ctx, u.NodeID)
	if err != nil {
		return nil, fmt.Errorf("look up user: %w", err)
	}
	if viewer.ID != u.ID {
		return nil, fmt.Errorf("node %s belongs to user %d, expected %d", u.NodeID, viewer.ID, u.ID)
	}

	if err := setPhase(ctx, s.db, scanID, "listing repositories", 0); err != nil {
		return nil, err
	}
	repos, err := gh.OwnedRepos(ctx, viewer.Login)
	if err != nil {
		return nil, err
	}

	var (
		mu       sync.Mutex
		warnings []string
	)
	warn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		log.Warn(msg)
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	if err := upsertRepos(ctx, s.db, u.ID, scanID, repos); err != nil {
		return nil, fmt.Errorf("upsert repos: %w", err)
	}

	// Scan watched public repositories (if any).
	watchedRows, err := listWatched(ctx, s.db, u.ID)
	if err != nil {
		warn("load watchlist: %v", err)
	}
	ownedIDs := map[int64]bool{}
	for _, r := range repos {
		ownedIDs[r.ID] = true
	}
	isWatched := map[int64]bool{}
	pubGH, useGraphQL := s.watchedClient(gh, log)
	if len(watchedRows) > 0 {
		if err := setPhase(ctx, s.db, scanID, "listing watched repositories", len(repos)+len(watchedRows)); err != nil {
			return nil, err
		}
		limit := len(watchedRows)
		if !useGraphQL && limit > maxUnauthWatched {
			warn("unauthenticated public API limit: scanning first %d of %d watched repos", maxUnauthWatched, limit)
			limit = maxUnauthWatched
		}
		var watchedRepos []github.Repo
		var watchedIDs []int64
		for _, w := range watchedRows[:limit] {
			r, err := pubGH.PublicRepo(ctx, w.Owner, w.Name, useGraphQL)
			if err != nil {
				_ = setWatchedError(ctx, s.db, w.ID, err.Error())
				warn("watched %s/%s: %v", w.Owner, w.Name, err)
				continue
			}
			_ = markWatchedScanned(ctx, s.db, w.ID, r.ID)
			if ownedIDs[r.ID] {
				continue // already scanned as an owned repo
			}
			isWatched[r.ID] = true
			watchedRepos = append(watchedRepos, *r)
			watchedIDs = append(watchedIDs, w.ID)
		}
		if len(watchedRepos) > 0 {
			if err := upsertWatchedRepos(ctx, s.db, u.ID, scanID, watchedRepos, watchedIDs); err != nil {
				return nil, fmt.Errorf("upsert watched repos: %w", err)
			}
			repos = append(repos, watchedRepos...)
		}
	}

	if err := setPhase(ctx, s.db, scanID, "reading file trees", len(repos)); err != nil {
		return nil, err
	}

	// Trees and manifests for non-fork, non-empty repos, batched via GraphQL.
	// Skip unauthenticated watched repos (GraphQL requires auth).
	var treeRefs []github.RepoRef
	for _, r := range repos {
		if !r.IsFork && !r.IsEmpty && (!isWatched[r.ID] || useGraphQL) {
			treeRefs = append(treeRefs, r.Ref)
		}
	}
	trees, err := gh.Trees(ctx, treeRefs, treeDepth)
	if err != nil {
		warn("file trees unavailable: %v", err)
		trees = map[github.RepoRef][]github.TreeEntry{}
	}
	files := map[github.RepoRef]detect.Files{}
	var manifestRefs []github.FileRef
	for _, r := range repos {
		f := detect.Inspect(toEntries(trees[r.Ref]), r.LicenseSPDX)
		files[r.Ref] = f
		for _, p := range f.Manifests {
			manifestRefs = append(manifestRefs, github.FileRef{Repo: r.Ref, Path: p})
		}
	}
	if err := setPhase(ctx, s.db, scanID, "reading manifests", len(repos)); err != nil {
		return nil, err
	}
	contents, err := gh.FileContents(ctx, manifestRefs)
	if err != nil {
		warn("manifest contents unavailable: %v", err)
	}
	byRepo := map[github.RepoRef]map[string]string{}
	for f, text := range contents {
		if byRepo[f.Repo] == nil {
			byRepo[f.Repo] = map[string]string{}
		}
		byRepo[f.Repo][f.Path] = text
	}

	if err := setPhase(ctx, s.db, scanID, "scanning repositories", len(repos)); err != nil {
		return nil, err
	}
	now := s.now()
	// A DB write failure aborts the scan: cancel wctx so workers stop early.
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	var (
		fatal     error
		fatalOnce sync.Once
		wg        sync.WaitGroup
		results   = make([]*repoResult, len(repos))
	)
	jobs := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if wctx.Err() != nil {
					continue
				}
				r := repos[i]
				repoGH := gh
				if isWatched[r.ID] {
					repoGH = pubGH
				}
				res := s.collect(wctx, repoGH, r, isWatched[r.ID], files[r.Ref], byRepo[r.Ref], viewer, scanID, now, warn)
				results[i] = res
				if err := writeRepo(wctx, s.db, res); err != nil {
					fatalOnce.Do(func() { fatal = fmt.Errorf("write %s: %w", r.Ref, err); wcancel() })
					continue
				}
				if err := repoDone(wctx, s.db, scanID, gh.Rate()); err != nil {
					warn("progress update: %v", err)
				}
			}
		}()
	}
send:
	for i := range repos {
		select {
		case jobs <- i:
		case <-wctx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait() // after this, reading fatal and results is race-free
	if err := ctx.Err(); err != nil {
		return warnings, err
	}
	if fatal != nil {
		return warnings, fatal
	}
	if err := evaluateTrends(ctx, s.db, u.ID, scanID, now, results); err != nil {
		warn("trend evaluation: %v", err)
	}
	return warnings, nil
}

func toEntries(t []github.TreeEntry) []detect.Entry {
	out := make([]detect.Entry, len(t))
	for i, e := range t {
		out[i] = detect.Entry{Path: e.Path, IsDir: e.IsDir}
	}
	return out
}

// watchedClient picks the best GitHub client for public watched repos:
// 1) the user's installation client, 2) GITHUB_PUBLIC_TOKEN, 3) unauthenticated.
func (s *Scanner) watchedClient(gh github.Client, log *slog.Logger) (github.Client, bool) {
	if gh != nil {
		return gh, true
	}
	cache := dbCache{db: s.db, userID: 0}
	if s.publicToken != "" {
		return github.New(s.publicToken, github.WithCache(cache), github.WithLogger(log)), true
	}
	return github.New("", github.WithCache(cache), github.WithLogger(log)), false
}

// collect gathers per-repo REST/GraphQL extras and computes health.
func (s *Scanner) collect(ctx context.Context, gh github.Client, r github.Repo, isWatched bool, f detect.Files,
	manifests map[string]string, viewer *github.Viewer, scanID int64, now time.Time,
	warn func(string, ...any)) *repoResult {

	res := &repoResult{Repo: r, Files: f, Login: viewer.Login, UserID: viewer.ID, ScanID: scanID}
	res.Tech = detect.Stack(f, manifests)
	for p, text := range manifests {
		if path.Base(p) == "package.json" && detect.HasRealTestScript(text) {
			res.Files.HasTests = true
		}
	}

	pushedRecently := r.PushedAt != nil && now.Sub(*r.PushedAt) < commitWindow
	if !r.IsEmpty && !r.IsFork && pushedRecently {
		weeks, ok, err := gh.CommitActivity(ctx, r.Ref)
		switch {
		case err != nil:
			warn("%s: commit activity: %v", r.Ref, err)
		case !ok:
			warn("%s: commit activity still being computed by GitHub", r.Ref)
		default:
			res.Weeks = weeks
		}
	}

	if !r.IsEmpty && !isWatched {
		since := now.Add(-commitWindow)
		last, err := lastCommitTime(ctx, s.db, r.ID)
		if err != nil {
			warn("%s: last commit lookup: %v", r.Ref, err)
		} else if last != nil && last.Add(-commitRescan).After(since) {
			since = last.Add(-commitRescan)
		}
		if r.PushedAt != nil && r.PushedAt.After(since) {
			commits, err := gh.CommitsBy(ctx, r.Ref, viewer.NodeID, since)
			if err != nil {
				warn("%s: commits: %v", r.Ref, err)
			}
			res.Commits = commits
		}
	}

	if res.Files.HasCI && !r.IsFork {
		runs, err := gh.FailedRuns(ctx, r.Ref, now.Add(-runsWindow))
		if err != nil {
			warn("%s: workflow runs: %v", r.Ref, err)
		}
		res.Runs = runs
	}

	branches := make([]health.Branch, len(r.Branches))
	for i, b := range r.Branches {
		branches[i] = health.Branch{Name: b.Name, LastCommitAt: b.LastCommitAt, IsDefault: b.Name == r.DefaultBranch}
	}
	prCreated := make([]time.Time, len(r.OpenPRs))
	for i, p := range r.OpenPRs {
		prCreated[i] = p.CreatedAt
	}
	res.Health = health.Evaluate(health.Input{
		HasReadme: res.Files.HasReadme, HasLicense: res.Files.HasLicense, HasCI: res.Files.HasCI,
		HasTests: res.Files.HasTests, HasDescription: strings.TrimSpace(r.Description) != "",
		TopicCount: len(r.Topics), PushedAt: r.PushedAt, Branches: branches, OpenPRCreated: prCreated,
	}, now)

	// Trend snapshot & decay-rule context.
	var staleNames []string
	for _, b := range branches {
		if !b.IsDefault && b.LastCommitAt != nil && now.Sub(*b.LastCommitAt) > health.StaleBranchAge {
			staleNames = append(staleNames, b.Name)
		}
	}
	commit7d, commit30d := commitCounts(res.Weeks, res.Commits, now)
	var uniqueAuthors30d, topAuthorPct int
	var topAuthorLogin string
	if !r.IsEmpty && !r.IsFork && pushedRecently {
		authors, err := gh.RecentCommitAuthors(ctx, r.Ref, now.Add(-30*24*time.Hour), 100)
		if err != nil {
			warn("%s: commit authors: %v", r.Ref, err)
		} else if len(authors) > 0 {
			counts := map[string]int{}
			for _, a := range authors {
				counts[a.Login]++
			}
			uniqueAuthors30d = len(counts)
			maxC := 0
			for login, c := range counts {
				if c > maxC {
					maxC = c
					topAuthorLogin = login
				}
			}
			topAuthorPct = maxC * 100 / len(authors)
			if commit30d == 0 {
				commit30d = len(authors)
			}
		}
	}
	res.TrendSnap = trends.Snapshot{
		RepoID: r.ID, ScanID: scanID, ScannedAt: now,
		HealthScore: res.Health.Score, OpenPRs: r.OpenPRCount, OpenIssues: r.OpenIssueCount,
		StaleBranches: len(staleNames), FailedRuns: len(res.Runs),
		CommitCount7d: commit7d, CommitCount30d: commit30d,
		UniqueAuthors30d: uniqueAuthors30d, TopAuthorLogin: topAuthorLogin, TopAuthorPct: topAuthorPct,
		Stars: r.Stars, Forks: r.Forks,
	}
	prItems := make([]trends.PRItem, len(r.OpenPRs))
	for i, p := range r.OpenPRs {
		prItems[i] = trends.PRItem{Number: p.Number, Title: p.Title, URL: p.URL, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	}
	runItems := make([]trends.RunItem, len(res.Runs))
	for i, run := range res.Runs {
		runItems[i] = trends.RunItem{Workflow: run.Name, Branch: run.Branch, URL: run.URL}
	}
	res.TrendCtx = trends.RepoContext{
		FullName: r.Ref.String(), DefaultBranch: r.DefaultBranch,
		HasCI: res.Files.HasCI, HasTests: res.Files.HasTests,
		IsFork: r.IsFork, IsArchived: r.IsArchived,
		OpenPRs: prItems, StaleBranchNames: staleNames, FailedRuns: runItems,
	}
	return res
}

func commitCounts(weeks []github.CommitWeek, commits []github.Commit, now time.Time) (c7, c30 int) {
	cut7 := now.Add(-7 * 24 * time.Hour)
	cut30 := now.Add(-30 * 24 * time.Hour)
	if len(weeks) > 0 {
		for _, w := range weeks {
			if !w.WeekStart.Before(cut30.Add(-7 * 24 * time.Hour)) {
				if w.WeekStart.Add(7 * 24 * time.Hour).After(cut30) {
					c30 += w.Total
				}
				if w.WeekStart.Add(7 * 24 * time.Hour).After(cut7) {
					c7 += w.Total
				}
			}
		}
		return c7, c30
	}
	for _, c := range commits {
		if c.AuthoredAt.After(cut30) {
			c30++
		}
		if c.AuthoredAt.After(cut7) {
			c7++
		}
	}
	return c7, c30
}
