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
)

const (
	treeDepth    = 4
	workers      = 3 // GitHub discourages heavy concurrency (secondary limits)
	scanTimeout  = 45 * time.Minute
	keepDetail   = 30
	commitWindow = 365 * 24 * time.Hour
	commitRescan = 7 * 24 * time.Hour  // overlap so late-pushed commits are caught
	runsWindow   = 14 * 24 * time.Hour // failed workflow runs kept for the digest
)

var ErrUnknownUser = errors.New("unknown user")

type ClientFactory func(token string, cache github.Cache) github.Client

type TokenSource interface {
	AccessToken(u *store.User) (string, error)
}

type Scanner struct {
	db        *pgxpool.Pool
	store     *store.Store
	tokens    TokenSource
	newClient ClientFactory
	log       *slog.Logger
	now       func() time.Time

	baseCtx context.Context
	wg      sync.WaitGroup
}

// New creates a scanner. Scans run under baseCtx, so cancelling it (server
// shutdown) stops them.
func New(baseCtx context.Context, db *pgxpool.Pool, st *store.Store, tokens TokenSource, f ClientFactory, log *slog.Logger) *Scanner {
	return &Scanner{db: db, store: st, tokens: tokens, newClient: f, log: log, now: time.Now, baseCtx: baseCtx}
}

// DefaultClientFactory builds the real HTTP client.
func DefaultClientFactory(log *slog.Logger) ClientFactory {
	return func(token string, cache github.Cache) github.Client {
		return github.New(token, github.WithCache(cache), github.WithLogger(log))
	}
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
		token, err := s.tokens.AccessToken(u)
		if err != nil {
			return nil, err
		}
		client = s.newClient(token, dbCache{db: s.db, userID: u.ID})
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
	viewer, err := gh.Viewer(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	if viewer.ID != u.ID {
		return nil, fmt.Errorf("token belongs to user %d, expected %d", viewer.ID, u.ID)
	}

	if err := setPhase(ctx, s.db, scanID, "listing repositories", 0); err != nil {
		return nil, err
	}
	repos, err := gh.OwnedRepos(ctx)
	if err != nil {
		return nil, err
	}
	if err := setPhase(ctx, s.db, scanID, "reading file trees", len(repos)); err != nil {
		return nil, err
	}
	if err := upsertRepos(ctx, s.db, u.ID, scanID, repos); err != nil {
		return nil, fmt.Errorf("upsert repos: %w", err)
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

	// Trees and manifests for non-fork, non-empty repos, batched via GraphQL.
	var treeRefs []github.RepoRef
	for _, r := range repos {
		if !r.IsFork && !r.IsEmpty {
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
				res := s.collect(wctx, gh, r, files[r.Ref], byRepo[r.Ref], viewer, scanID, now, warn)
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
	wg.Wait() // after this, reading fatal is race-free
	if err := ctx.Err(); err != nil {
		return warnings, err
	}
	return warnings, fatal
}

func toEntries(t []github.TreeEntry) []detect.Entry {
	out := make([]detect.Entry, len(t))
	for i, e := range t {
		out[i] = detect.Entry{Path: e.Path, IsDir: e.IsDir}
	}
	return out
}

// collect gathers per-repo REST/GraphQL extras and computes health.
func (s *Scanner) collect(ctx context.Context, gh github.Client, r github.Repo, f detect.Files,
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

	if !r.IsEmpty {
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
	return res
}
