package prreview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// --- fakes ---

type fakeGH struct {
	mu      sync.Mutex
	prs     map[int]*github.PullRequest
	files   []github.PRFile
	commits []string
	labels  []string
	calls   int
	failPR  error
}

func (f *fakeGH) PullRequest(_ context.Context, _ github.RepoRef, n int) (*github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failPR != nil {
		return nil, f.failPR
	}
	p, ok := f.prs[n]
	if !ok {
		return nil, &github.APIError{Status: http.StatusNotFound}
	}
	cp := *p
	return &cp, nil
}
func (f *fakeGH) OpenPullRequests(context.Context, github.RepoRef) ([]github.PullRequest, error) {
	var out []github.PullRequest
	for _, p := range f.prs {
		if p.State == "open" {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (f *fakeGH) PullRequestFiles(context.Context, github.RepoRef, int) ([]github.PRFile, error) {
	return f.files, nil
}
func (f *fakeGH) PullRequestCommitMessages(context.Context, github.RepoRef, int) ([]string, error) {
	return f.commits, nil
}
func (f *fakeGH) Labels(context.Context, github.RepoRef) ([]string, error) { return f.labels, nil }

// fakeComments is an in-memory issue-comment store.
type fakeComments struct {
	mu       sync.Mutex
	next     int64
	comments map[int64]github.IssueComment
	created  int
	updated  int
}

func newFakeComments() *fakeComments {
	return &fakeComments{next: 100, comments: map[int64]github.IssueComment{}}
}
func (c *fakeComments) IssueComments(context.Context, github.RepoRef, int) ([]github.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []github.IssueComment
	for _, ic := range c.comments {
		out = append(out, ic)
	}
	return out, nil
}
func (c *fakeComments) CreateIssueComment(_ context.Context, _ github.RepoRef, _ int, body string) (*github.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	c.created++
	ic := github.IssueComment{ID: c.next, Body: body, Author: "gitpulse-dev[bot]", HTMLURL: "https://x/c/" + string(rune('0'+c.next%10))}
	c.comments[ic.ID] = ic
	return &ic, nil
}
func (c *fakeComments) UpdateIssueComment(_ context.Context, _ github.RepoRef, id int64, body string) (*github.IssueComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ic, ok := c.comments[id]
	if !ok {
		return nil, &github.APIError{Status: http.StatusNotFound}
	}
	c.updated++
	ic.Body = body
	c.comments[id] = ic
	return &ic, nil
}

type fakeAccess struct {
	gh       *fakeGH
	comments *fakeComments
	writers  int
}

func (a *fakeAccess) Read(context.Context, *store.User) (GitHub, error) { return a.gh, nil }
func (a *fakeAccess) Writer(context.Context, *store.User, int64) (Commenter, error) {
	a.writers++
	return a.comments, nil
}
func (a *fakeAccess) BotLogin(context.Context) (string, error) { return "gitpulse-dev[bot]", nil }

type fakeLLM struct{ reply string }

func (f fakeLLM) Name() string  { return "fake" }
func (f fakeLLM) Model() string { return "fake" }
func (f fakeLLM) Complete(context.Context, llm.ProviderRequest) (llm.ProviderResponse, error) {
	return llm.ProviderResponse{Text: f.reply, InputTokens: 100, OutputTokens: 50}, nil
}

// --- setup ---

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const repoID = 77

func setup(t *testing.T, provider llm.Provider) (*Service, *pgxpool.Pool, *fakeAccess) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := context.Background()
	for _, q := range []string{
		`TRUNCATE users, repos, pr_reviews, pr_comments, llm_cache CASCADE`,
		`INSERT INTO users (id, node_id, login) VALUES (1, 'U1', 'me')`,
		`INSERT INTO repos (id, user_id, full_name) VALUES (77, 1, 'me/sandbox')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	gh := &fakeGH{
		prs: map[int]*github.PullRequest{1: {Number: 1, Title: "fix: crash on empty repo", State: "open", HeadSHA: "sha1", Author: "friend", ChangedFiles: 2, Additions: 5, Deletions: 1}},
		files: []github.PRFile{
			{Path: "db/migrations/0002.sql", Status: "added", Changes: 4, Patch: "+create table x"},
			{Path: "package-lock.json", Status: "modified", Changes: 2, Patch: "+lock"},
		},
		labels: []string{"bug", "enhancement"},
	}
	access := &fakeAccess{gh: gh, comments: newFakeComments()}
	ai := llm.New(provider, pool, llm.Config{MonthlyBudgetUSD: 5, Price: llm.Price{InputPerMTok: 1, OutputPerMTok: 1}}, quiet)
	return New(pool, store.New(pool), access, ai, quiet), pool, access
}

func job(sha string) Job {
	return Job{UserID: 1, RepoID: repoID, RepoName: "me/sandbox", Number: 1, HeadSHA: sha, Title: "t", Source: "webhook"}
}

func setMode(t *testing.T, pool *pgxpool.Pool, mode string) {
	t.Helper()
	if _, err := store.New(pool).SaveRepoSetting(context.Background(), 1, store.RepoSetting{RepoID: repoID, PRMode: mode}); err != nil {
		t.Fatal(err)
	}
}

func drain(t *testing.T, s *Service) {
	t.Helper()
	for range 20 {
		worked, err := s.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("queue did not drain")
}

func reviewRow(t *testing.T, s *Service, sha string) ReviewRow {
	t.Helper()
	rows, err := s.Recent(context.Background(), 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.HeadSHA == sha {
			return r
		}
	}
	t.Fatalf("no review for %s", sha)
	return ReviewRow{}
}

// --- tests ---

func TestEnqueueIsIdempotent(t *testing.T) {
	s, pool, _ := setup(t, nil)
	ctx := context.Background()
	c1, err := s.Enqueue(ctx, job("sha1"))
	if err != nil || !c1 {
		t.Fatalf("first: %v %v", c1, err)
	}
	// Same repo+PR+SHA from the catch-up sync: no duplicate.
	j := job("sha1")
	j.Source = "sync"
	if c2, _ := s.Enqueue(ctx, j); c2 {
		t.Fatal("duplicate review created")
	}
	if c3, _ := s.Enqueue(ctx, job("sha2")); !c3 {
		t.Fatal("new head SHA must create a review")
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM pr_reviews`).Scan(&n)
	if n != 2 {
		t.Fatalf("rows = %d", n)
	}
}

func TestDashboardModeNoKeyPostsNothing(t *testing.T) {
	s, _, access := setup(t, nil)
	_, _ = s.Enqueue(context.Background(), job("sha1"))
	drain(t, s)
	r := reviewRow(t, s, "sha1")
	if r.Status != "done" || r.Mode != "dashboard" || r.AIStatus != "disabled" || r.CommentURL != "" {
		t.Fatalf("row = %+v", r)
	}
	if access.writers != 0 || access.comments.created != 0 {
		t.Fatal("dashboard mode must not write to GitHub")
	}
	var res Result
	_ = json.Unmarshal(r.Result, &res)
	if res.AI != nil || len(res.Flags) == 0 || res.Flags[0].Kind != "migration" ||
		len(res.SuggestedLabels) != 1 || res.SuggestedLabels[0] != "bug" || res.Omitted[0].Reason != "lock file" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCommentModeKeepsOneComment(t *testing.T) {
	s, pool, access := setup(t, nil)
	setMode(t, pool, store.PRModeComment)
	ctx := context.Background()

	_, _ = s.Enqueue(ctx, job("sha1"))
	drain(t, s)
	if access.comments.created != 1 || reviewRow(t, s, "sha1").CommentURL == "" {
		t.Fatalf("first push: created=%d", access.comments.created)
	}

	// New push: same comment edited, nothing new posted.
	access.gh.prs[1].HeadSHA = "sha2"
	_, _ = s.Enqueue(ctx, job("sha2"))
	drain(t, s)
	if access.comments.created != 1 || access.comments.updated != 1 {
		t.Fatalf("second push: created=%d updated=%d", access.comments.created, access.comments.updated)
	}
	for _, c := range access.comments.comments {
		if !strings.Contains(c.Body, "`sha2`") {
			t.Fatalf("comment not updated to new SHA: %s", c.Body)
		}
	}

	// Someone deleted our stored comment, but another marked one exists
	// (e.g. the DB was restored): it's found by marker and edited.
	var stored int64
	_ = pool.QueryRow(ctx, `SELECT comment_id FROM pr_comments`).Scan(&stored)
	delete(access.comments.comments, stored)
	access.comments.comments[555] = github.IssueComment{ID: 555, Body: Marker + "\nold", Author: "gitpulse-dev[bot]"}
	// A user quoting the marker must not be mistaken for ours.
	access.comments.comments[556] = github.IssueComment{ID: 556, Body: "> " + Marker, Author: "someone"}
	access.gh.prs[1].HeadSHA = "sha3"
	_, _ = s.Enqueue(ctx, job("sha3"))
	drain(t, s)
	if access.comments.created != 1 || !strings.Contains(access.comments.comments[555].Body, "`sha3`") ||
		strings.Contains(access.comments.comments[556].Body, "sha3") {
		t.Fatalf("marker lookup failed: created=%d", access.comments.created)
	}
	_ = pool.QueryRow(ctx, `SELECT comment_id FROM pr_comments`).Scan(&stored)
	if stored != 555 {
		t.Fatalf("stored id = %d", stored)
	}
}

func TestSupersededClosedAndOff(t *testing.T) {
	s, pool, access := setup(t, nil)
	ctx := context.Background()
	// The webhook for sha1 arrives after a newer push.
	access.gh.prs[1].HeadSHA = "sha9"
	_, _ = s.Enqueue(ctx, job("sha1"))
	drain(t, s)
	if r := reviewRow(t, s, "sha1"); r.Status != "skipped" || !strings.HasPrefix(r.SkipReason, "superseded") {
		t.Fatalf("superseded: %+v", r)
	}

	access.gh.prs[1].HeadSHA, access.gh.prs[1].State = "sha2", "closed"
	_, _ = s.Enqueue(ctx, job("sha2"))
	drain(t, s)
	if r := reviewRow(t, s, "sha2"); r.Status != "skipped" || r.SkipReason != "PR is closed" {
		t.Fatalf("closed: %+v", r)
	}

	setMode(t, pool, store.PRModeOff)
	before := access.gh.calls
	_, _ = s.Enqueue(ctx, job("sha3"))
	drain(t, s)
	if r := reviewRow(t, s, "sha3"); r.Status != "skipped" || access.gh.calls != before {
		t.Fatalf("off: %+v calls=%d", r, access.gh.calls-before)
	}
}

func TestRetriesThenFails(t *testing.T) {
	s, pool, access := setup(t, nil)
	access.gh.failPR = errors.New("github is down")
	_, _ = s.Enqueue(context.Background(), job("sha1"))
	drain(t, s)
	r := reviewRow(t, s, "sha1")
	var attempts int
	_ = pool.QueryRow(context.Background(), `SELECT attempts FROM pr_reviews WHERE id = $1`, r.ID).Scan(&attempts)
	if r.Status != "failed" || attempts != maxAttempts || !strings.Contains(r.Error, "github is down") {
		t.Fatalf("row=%+v attempts=%d", r, attempts)
	}
}

func TestAIOutputIsSanitisedAndLabelsFiltered(t *testing.T) {
	reply := `{"summary":"Fixes a crash. Ping @octocat, see [here](https://evil.example).","notable":["Adds <b>table</b> x"],
		"suggestedTitle":"fix: handle empty repos <!-- gitpulse:pr-review -->","suggestedLabels":["bug","priority:high","Enhancement"]}`
	s, pool, access := setup(t, fakeLLM{reply: reply})
	setMode(t, pool, store.PRModeComment)
	_, _ = s.Enqueue(context.Background(), job("sha1"))
	drain(t, s)
	r := reviewRow(t, s, "sha1")
	var res Result
	_ = json.Unmarshal(r.Result, &res)
	if r.AIStatus != "ok" || res.AI == nil {
		t.Fatalf("ai: %s %+v", r.AIStatus, res.AI)
	}
	if strings.Contains(res.AI.Summary, "evil.example") || strings.Contains(res.AI.Summary, "@octocat") ||
		strings.Contains(res.AI.Notable[0], "<b>") || strings.Contains(res.AI.SuggestedTitle, "<!--") {
		t.Fatalf("unsanitised: %+v", res.AI)
	}
	if strings.Join(res.AI.SuggestedLabels, ",") != "bug,enhancement" {
		t.Fatalf("labels = %v", res.AI.SuggestedLabels)
	}
	for _, c := range access.comments.comments {
		if strings.Count(c.Body, Marker) != 1 || !strings.Contains(c.Body, "#### Summary") {
			t.Fatalf("comment:\n%s", c.Body)
		}
	}
}

func TestSyncQueuesOnlyUnreviewed(t *testing.T) {
	s, _, access := setup(t, nil)
	access.gh.prs[2] = &github.PullRequest{Number: 2, State: "open", HeadSHA: "shaB"}
	u := &store.User{ID: 1, Login: "me"}
	res, err := s.Sync(context.Background(), u)
	if err != nil || res.ReposChecked != 1 || res.OpenPRs != 2 || res.Queued != 2 {
		t.Fatalf("first sync: %+v %v", res, err)
	}
	res, _ = s.Sync(context.Background(), u)
	if res.Queued != 0 || res.AlreadyReviewed != 2 {
		t.Fatalf("second sync: %+v", res)
	}
}
