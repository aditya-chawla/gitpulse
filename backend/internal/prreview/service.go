package prreview

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// VerifySignature checks GitHub's X-Hub-Signature-256 header (HMAC-SHA256 of
// the raw body with the webhook secret) in constant time.
func VerifySignature(secret, body []byte, header string) bool {
	if len(secret) == 0 || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// GitHub is everything the worker needs from GitHub; *github.HTTPClient
// implements it. Tests use a fake.
type GitHub interface {
	PullRequest(ctx context.Context, repo github.RepoRef, number int) (*github.PullRequest, error)
	OpenPullRequests(ctx context.Context, repo github.RepoRef) ([]github.PullRequest, error)
	PullRequestFiles(ctx context.Context, repo github.RepoRef, number int) ([]github.PRFile, error)
	PullRequestCommitMessages(ctx context.Context, repo github.RepoRef, number int) ([]string, error)
	Labels(ctx context.Context, repo github.RepoRef) ([]string, error)
}

// Access provides GitHub clients for a user. Read covers every installed
// repo read-only; Writer returns a client whose token can only write PR
// comments on one repo.
type Access interface {
	Read(ctx context.Context, u *store.User) (GitHub, error)
	Writer(ctx context.Context, u *store.User, repoID int64) (Commenter, error)
	BotLogin(ctx context.Context) (string, error)
}

const (
	maxAttempts     = 3
	syncConcurrency = 6 // GitHub discourages heavy concurrency (secondary limits)
)

type Service struct {
	db     *pgxpool.Pool
	store  *store.Store
	access Access
	ai     *llm.Service
	log    *slog.Logger

	wake chan struct{}
	wg   sync.WaitGroup
}

func New(db *pgxpool.Pool, st *store.Store, access Access, ai *llm.Service, log *slog.Logger) *Service {
	return &Service{db: db, store: st, access: access, ai: ai, log: log, wake: make(chan struct{}, 1)}
}

// Job identifies one review: a PR at one head commit.
type Job struct {
	UserID   int64
	RepoID   int64
	RepoName string // owner/name
	Number   int
	HeadSHA  string
	Title    string
	Author   string
	URL      string
	Source   string // webhook | sync | manual
}

// Enqueue records a pending review. A second delivery for the same repo, PR
// and head SHA is a no-op (created=false): webhooks and catch-up syncs can
// overlap freely.
func (s *Service) Enqueue(ctx context.Context, j Job) (created bool, err error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO pr_reviews (user_id, repo_id, repo_name, pr_number, head_sha, title, author, url, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (repo_id, pr_number, head_sha) DO NOTHING`,
		j.UserID, j.RepoID, j.RepoName, j.Number, j.HeadSHA, j.Title, j.Author, j.URL, j.Source)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		s.Kick()
		return true, nil
	}
	return false, nil
}

// Kick wakes the worker without blocking.
func (s *Service) Kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Start runs the single background worker until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			for ctx.Err() == nil {
				worked, err := s.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					s.log.Error("pr review worker", "err", err)
					break
				}
				if !worked {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-t.C:
			}
		}
	}()
}

// Wait blocks until the worker exits or the timeout passes.
func (s *Service) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

type review struct {
	ID       int64
	UserID   int64
	RepoID   int64
	RepoName string
	Number   int
	HeadSHA  string
	Attempts int
}

// claim takes the oldest pending review (or one stuck running for over 10
// minutes after a crash) and marks it running.
func (s *Service) claim(ctx context.Context) (*review, error) {
	var r review
	err := s.db.QueryRow(ctx, `
		UPDATE pr_reviews SET status = 'running', attempts = attempts + 1, updated_at = now()
		WHERE id = (
			SELECT id FROM pr_reviews
			WHERE status = 'pending' OR (status = 'running' AND updated_at < now() - interval '10 minutes')
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, user_id, repo_id, repo_name, pr_number, head_sha, attempts`).
		Scan(&r.ID, &r.UserID, &r.RepoID, &r.RepoName, &r.Number, &r.HeadSHA, &r.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// RunOnce processes one queued review. worked=false means the queue is empty.
func (s *Service) RunOnce(ctx context.Context) (worked bool, err error) {
	r, err := s.claim(ctx)
	if err != nil || r == nil {
		return false, err
	}
	log := s.log.With("review_id", r.ID, "repo", r.RepoName, "pr", r.Number, "sha", short(r.HeadSHA))
	outcome, perr := s.process(ctx, r)
	fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch {
	case perr == nil:
		log.Info("pr reviewed", "status", outcome.status, "mode", outcome.mode, "ai", outcome.aiStatus, "skip", outcome.skipReason)
		_, err = s.db.Exec(fctx, `
			UPDATE pr_reviews SET status = $2, mode = nullif($3,''), result = $4, ai_status = nullif($5,''),
			  skip_reason = nullif($6,''), comment_url = nullif($7,''), title = coalesce(nullif($8,''), title),
			  error = NULL, updated_at = now()
			WHERE id = $1`, r.ID, outcome.status, outcome.mode, outcome.result, outcome.aiStatus,
			outcome.skipReason, outcome.commentURL, outcome.title)
	case r.Attempts >= maxAttempts || errors.Is(perr, errPermanent):
		log.Error("pr review failed", "err", perr, "attempts", r.Attempts)
		_, err = s.db.Exec(fctx, `UPDATE pr_reviews SET status = 'failed', error = $2, updated_at = now() WHERE id = $1`, r.ID, truncate(perr.Error(), 1000))
	default:
		log.Warn("pr review will retry", "err", perr, "attempts", r.Attempts)
		_, err = s.db.Exec(fctx, `UPDATE pr_reviews SET status = 'pending', error = $2, updated_at = now() WHERE id = $1`, r.ID, truncate(perr.Error(), 1000))
	}
	return true, err
}

var errPermanent = errors.New("permanent")

type outcome struct {
	status, mode, aiStatus, skipReason, commentURL, title string
	result                                                []byte
}

func repoRef(fullName string) github.RepoRef {
	owner, name, _ := strings.Cut(fullName, "/")
	return github.RepoRef{Owner: owner, Name: name}
}

func (s *Service) process(ctx context.Context, r *review) (outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	u, err := s.store.GetUser(ctx, r.UserID)
	if err != nil {
		return outcome{}, fmt.Errorf("%w: user: %v", errPermanent, err)
	}
	mode := store.PRModeDashboard
	if rs, err := s.store.RepoSettingFor(ctx, r.UserID, r.RepoID); err == nil {
		mode = rs.PRMode
	} else if !errors.Is(err, store.ErrNotFound) {
		return outcome{}, err
	}
	if mode == store.PRModeOff {
		return outcome{status: "skipped", mode: mode, skipReason: "PR assistant is off for this repo"}, nil
	}

	gh, err := s.access.Read(ctx, u)
	if err != nil {
		return outcome{}, err
	}
	repo := repoRef(r.RepoName)
	pr, err := gh.PullRequest(ctx, repo, r.Number)
	if err != nil {
		if github.IsNotFound(err) {
			return outcome{status: "skipped", mode: mode, skipReason: "PR not found"}, nil
		}
		return outcome{}, err
	}
	switch {
	case pr.State != "open":
		return outcome{status: "skipped", mode: mode, skipReason: "PR is " + pr.State, title: pr.Title}, nil
	case pr.HeadSHA != r.HeadSHA:
		// A newer push has (or will have) its own review row.
		return outcome{status: "skipped", mode: mode, skipReason: "superseded by " + short(pr.HeadSHA), title: pr.Title}, nil
	}
	files, err := gh.PullRequestFiles(ctx, repo, r.Number)
	if err != nil {
		return outcome{}, err
	}
	commits, err := gh.PullRequestCommitMessages(ctx, repo, r.Number)
	if err != nil {
		return outcome{}, err
	}
	labels, err := gh.Labels(ctx, repo)
	if err != nil {
		return outcome{}, err
	}

	lim := DiffLimitsFor(s.ai)
	a, diff := Analyze(*pr, commits, files, labels, lim)
	res := Result{Analysis: a, HeadSHA: r.HeadSHA}
	ai, status := summarize(ctx, s.ai, r.UserID, r.RepoID, *pr, commits, labels, a, diff)
	res.AI = ai

	o := outcome{status: "done", mode: mode, aiStatus: status, title: pr.Title}
	if o.result, err = json.Marshal(res); err != nil {
		return outcome{}, err
	}
	if mode == store.PRModeComment {
		url, err := s.comment(ctx, u, r, RenderComment(res, aiNote(status)))
		if err != nil {
			return outcome{}, err
		}
		o.commentURL = url
	}
	return o, nil
}

// DiffLimitsFor sizes the AI diff to the LLM input cap (the same limits are
// used without AI, so the omitted-files list doesn't depend on the key).
func DiffLimitsFor(ai *llm.Service) DiffLimits {
	maxTokens := 24_000
	if ai != nil {
		maxTokens = ai.MaxInputTokens()
	}
	total := maxTokens*3 - 12_000 // room for the system prompt, title, body and commits
	if total < 8_000 {
		total = 8_000
	}
	return DiffLimits{PerFileChars: 6_000, TotalChars: total}
}

func (s *Service) comment(ctx context.Context, u *store.User, r *review, body string) (string, error) {
	bot, err := s.access.BotLogin(ctx)
	if err != nil {
		return "", err
	}
	w, err := s.access.Writer(ctx, u, r.RepoID)
	if err != nil {
		return "", err
	}
	var stored int64
	err = s.db.QueryRow(ctx, `SELECT comment_id FROM pr_comments WHERE repo_id = $1 AND pr_number = $2`, r.RepoID, r.Number).Scan(&stored)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	ic, err := UpsertComment(ctx, w, repoRef(r.RepoName), r.Number, bot, stored, body)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO pr_comments (repo_id, pr_number, comment_id, html_url) VALUES ($1,$2,$3,$4)
		ON CONFLICT (repo_id, pr_number) DO UPDATE SET comment_id = EXCLUDED.comment_id, html_url = EXCLUDED.html_url, updated_at = now()`,
		r.RepoID, r.Number, ic.ID, ic.HTMLURL)
	return ic.HTMLURL, err
}

// SyncResult reports what a catch-up pass found.
type SyncResult struct {
	UserID          int64 `json:"userId"`
	ReposChecked    int   `json:"reposChecked"`
	OpenPRs         int   `json:"openPrs"`
	Queued          int   `json:"queued"`
	AlreadyReviewed int   `json:"alreadyReviewed"`
	Errors          int   `json:"errors"`
}

// Sync enqueues every open PR whose head commit hasn't been reviewed yet, in
// repos where the PR assistant isn't off. It covers webhooks missed while
// the free instance was asleep.
func (s *Service) Sync(ctx context.Context, u *store.User) (SyncResult, error) {
	res := SyncResult{UserID: u.ID}
	repos, err := s.store.ListRepoSettings(ctx, u.ID)
	if err != nil {
		return res, err
	}
	gh, err := s.access.Read(ctx, u)
	if err != nil {
		return res, err
	}
	// List open PRs with bounded concurrency (most answers are cheap 304s
	// thanks to ETags), then enqueue sequentially.
	type listed struct {
		rs  store.RepoSetting
		prs []github.PullRequest
		err error
	}
	var targets []store.RepoSetting
	for _, rs := range repos {
		if rs.PRMode != store.PRModeOff && !rs.IsArchived {
			targets = append(targets, rs)
		}
	}
	results := make([]listed, len(targets))
	sem := make(chan struct{}, syncConcurrency)
	var wg sync.WaitGroup
	for i, rs := range targets {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			prs, err := gh.OpenPullRequests(ctx, repoRef(rs.FullName))
			results[i] = listed{rs: rs, prs: prs, err: err}
		})
	}
	wg.Wait()
	for _, l := range results {
		res.ReposChecked++
		if l.err != nil {
			res.Errors++
			s.log.Warn("pr sync: list open PRs", "repo", l.rs.FullName, "err", l.err)
			continue
		}
		for _, pr := range l.prs {
			res.OpenPRs++
			created, err := s.Enqueue(ctx, Job{UserID: u.ID, RepoID: l.rs.RepoID, RepoName: l.rs.FullName, Number: pr.Number,
				HeadSHA: pr.HeadSHA, Title: pr.Title, Author: pr.Author, URL: pr.URL, Source: "sync"})
			switch {
			case err != nil:
				return res, err
			case created:
				res.Queued++
			default:
				res.AlreadyReviewed++
			}
		}
	}
	return res, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- read model for the PRs page ---

type ReviewRow struct {
	ID         int64           `json:"id"`
	RepoID     int64           `json:"repoId"`
	Repo       string          `json:"repo"`
	Number     int             `json:"number"`
	HeadSHA    string          `json:"headSha"`
	Title      string          `json:"title"`
	Author     string          `json:"author"`
	URL        string          `json:"url"`
	Source     string          `json:"source"`
	Status     string          `json:"status"`
	Mode       string          `json:"mode,omitempty"`
	AIStatus   string          `json:"aiStatus,omitempty"`
	SkipReason string          `json:"skipReason,omitempty"`
	Error      string          `json:"error,omitempty"`
	CommentURL string          `json:"commentUrl,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

const reviewCols = `id, repo_id, repo_name, pr_number, head_sha, coalesce(title,''), coalesce(author,''), coalesce(url,''),
	source, status, coalesce(mode,''), coalesce(ai_status,''), coalesce(skip_reason,''), coalesce(error,''),
	coalesce(comment_url,''), result, created_at, updated_at`

func scanReview(row pgx.Row) (ReviewRow, error) {
	var r ReviewRow
	var result []byte
	err := row.Scan(&r.ID, &r.RepoID, &r.Repo, &r.Number, &r.HeadSHA, &r.Title, &r.Author, &r.URL, &r.Source, &r.Status,
		&r.Mode, &r.AIStatus, &r.SkipReason, &r.Error, &r.CommentURL, &result, &r.CreatedAt, &r.UpdatedAt)
	if len(result) > 0 {
		r.Result = result
	}
	return r, err
}

// Recent lists the user's latest reviews, newest first.
func (s *Service) Recent(ctx context.Context, userID int64, limit int) ([]ReviewRow, error) {
	rows, err := s.db.Query(ctx, `SELECT `+reviewCols+` FROM pr_reviews WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReviewRow{}
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns one review if it belongs to the user.
func (s *Service) Get(ctx context.Context, userID, id int64) (ReviewRow, error) {
	r, err := scanReview(s.db.QueryRow(ctx, `SELECT `+reviewCols+` FROM pr_reviews WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, store.ErrNotFound
	}
	return r, err
}
