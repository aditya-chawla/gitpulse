package scanner

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// fakeGH is an in-memory github.Client.
type fakeGH struct {
	viewer   github.Viewer
	repos    []github.Repo
	public   map[github.RepoRef]github.Repo
	trees    map[github.RepoRef][]github.TreeEntry
	files    map[github.FileRef]string
	weeks    []github.CommitWeek
	commits  map[github.RepoRef][]github.Commit
	authors  map[github.RepoRef][]github.CommitAuthor
	runs     []github.WorkflowRun
	sinceArg map[github.RepoRef]time.Time
}

func (f *fakeGH) User(context.Context, string) (*github.Viewer, error) { v := f.viewer; return &v, nil }
func (f *fakeGH) OwnedRepos(context.Context, string) ([]github.Repo, error) {
	return f.repos, nil
}
func (f *fakeGH) PublicRepo(_ context.Context, owner, name string, _ bool) (*github.Repo, error) {
	ref := github.RepoRef{Owner: owner, Name: name}
	if r, ok := f.public[ref]; ok {
		return &r, nil
	}
	return nil, &github.APIError{Status: 404, URL: "/repos/" + ref.String(), Body: "not found"}
}
func (f *fakeGH) Trees(_ context.Context, refs []github.RepoRef, _ int) (map[github.RepoRef][]github.TreeEntry, error) {
	out := map[github.RepoRef][]github.TreeEntry{}
	for _, r := range refs {
		if t, ok := f.trees[r]; ok {
			out[r] = t
		}
	}
	return out, nil
}
func (f *fakeGH) FileContents(_ context.Context, refs []github.FileRef) (map[github.FileRef]string, error) {
	out := map[github.FileRef]string{}
	for _, r := range refs {
		if t, ok := f.files[r]; ok {
			out[r] = t
		}
	}
	return out, nil
}
func (f *fakeGH) CommitActivity(context.Context, github.RepoRef) ([]github.CommitWeek, bool, error) {
	return f.weeks, true, nil
}
func (f *fakeGH) CommitsBy(_ context.Context, r github.RepoRef, _ string, since time.Time) ([]github.Commit, error) {
	f.sinceArg[r] = since
	return f.commits[r], nil
}
func (f *fakeGH) FailedRuns(context.Context, github.RepoRef, time.Time) ([]github.WorkflowRun, error) {
	return f.runs, nil
}
func (f *fakeGH) RecentCommitAuthors(_ context.Context, r github.RepoRef, _ time.Time, _ int) ([]github.CommitAuthor, error) {
	return f.authors[r], nil
}
func (f *fakeGH) Rate() github.RateInfo { return github.RateInfo{Calls: 7, GraphQLRemaining: 4990} }

func testDB(t *testing.T) *pgxpool.Pool {
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
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, scans, repos CASCADE`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func ptr(t time.Time) *time.Time { return &t }

func TestScanEndToEnd(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	st := store.New(pool)
	if err := st.UpsertUser(ctx, store.User{ID: 1, NodeID: "U_1", Login: "me"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	app := github.RepoRef{Owner: "me", Name: "app"}
	fork := github.RepoRef{Owner: "me", Name: "fork"}
	fake := &fakeGH{
		viewer: github.Viewer{ID: 1, NodeID: "U_1", Login: "me"},
		repos: []github.Repo{
			{ID: 10, Ref: app, Visibility: "public", Description: "an app", Topics: []string{"go"}, DefaultBranch: "main",
				CreatedAt: now.AddDate(-2, 0, 0), PushedAt: ptr(now.AddDate(0, 0, -3)), Stars: 5, LicenseSPDX: "MIT",
				Languages:   []github.LanguageBytes{{Language: "Go", Bytes: 900}, {Language: "TypeScript", Bytes: 100}},
				OpenPRCount: 1, OpenPRs: []github.Item{{Number: 4, Title: "fix", Author: "friend", URL: "u",
					ReviewRequests: []string{"me"}, CreatedAt: now.AddDate(0, 0, -40), UpdatedAt: now}},
				OpenIssueCount: 1, OpenIssues: []github.Item{{Number: 5, Title: "bug", URL: "u", CreatedAt: now, UpdatedAt: now}},
				Branches:    []github.Branch{{Name: "main", LastCommitAt: ptr(now)}, {Name: "old", LastCommitAt: ptr(now.AddDate(-1, 0, 0))}},
				RecentForks: []github.Actor{{Login: "fan", At: now}}},
			{ID: 11, Ref: fork, IsFork: true, Visibility: "public", CreatedAt: now.AddDate(-1, 0, 0), PushedAt: ptr(now.AddDate(-1, 0, 0))},
		},
		trees: map[github.RepoRef][]github.TreeEntry{app: {
			{Path: "README.md"}, {Path: ".github", IsDir: true}, {Path: ".github/workflows", IsDir: true},
			{Path: ".github/workflows/ci.yml"}, {Path: "go.mod"}, {Path: "main_test.go"},
		}},
		files:    map[github.FileRef]string{{Repo: app, Path: "go.mod"}: "module x\nrequire github.com/go-chi/chi/v5 v5.0.0\n"},
		weeks:    []github.CommitWeek{{WeekStart: now.AddDate(0, 0, -7), Total: 3, Days: [7]int{0, 1, 2, 0, 0, 0, 0}}},
		commits:  map[github.RepoRef][]github.Commit{app: {{SHA: "abc", AuthoredAt: now.AddDate(0, 0, -3), Additions: 10}}},
		runs:     []github.WorkflowRun{{ID: 99, Name: "ci", Conclusion: "failure", URL: "u", CreatedAt: now}},
		sinceArg: map[github.RepoRef]time.Time{},
	}
	s := New(ctx, pool, st, func(context.Context, *store.User, github.Cache) (github.Client, error) { return fake, nil },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }

	runScan := func() *Scan {
		t.Helper()
		id, existing, err := s.Start(ctx, 1, "ui")
		if err != nil || existing {
			t.Fatalf("start: id=%d existing=%v err=%v", id, existing, err)
		}
		s.Wait(10 * time.Second)
		sc, err := GetScan(ctx, pool, id)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Status != "succeeded" || sc.ReposDone != 2 || sc.ReposTotal != 2 || sc.APICalls != 7 {
			t.Fatalf("scan = %+v", sc)
		}
		return sc
	}
	first := runScan()

	var score int
	var hasCI, hasTests, hasReadme bool
	var status string
	if err := pool.QueryRow(ctx, `SELECT health_score, has_ci, has_tests, has_readme, status FROM repo_snapshots WHERE scan_id=$1 AND repo_id=10`,
		first.ID).Scan(&score, &hasCI, &hasTests, &hasReadme, &status); err != nil {
		t.Fatal(err)
	}
	// 15 readme + 10 license + 15 ci + 15 tests + 15 activity + 7 (1 stale) + 5 (1 old PR) + 10 meta
	if score != 92 || !hasCI || !hasTests || !hasReadme || status != "active" {
		t.Fatalf("snapshot score=%d ci=%v tests=%v readme=%v status=%s", score, hasCI, hasTests, hasReadme, status)
	}

	counts := map[string]int{}
	for _, tbl := range []string{"repo_languages", "repo_commit_weeks", "repo_branches", "repo_items", "repo_tech", "workflow_runs", "commits", "repo_stargazers"} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[tbl] = n
	}
	want := map[string]int{"repo_languages": 2, "repo_commit_weeks": 1, "repo_branches": 2, "repo_items": 2,
		"repo_tech": 2 /* GitHub Actions + chi */, "workflow_runs": 1, "commits": 1, "repo_stargazers": 1}
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("%s = %d, want %d", k, counts[k], v)
		}
	}
	var reviewReq bool
	_ = pool.QueryRow(ctx, `SELECT review_requested_from_me FROM repo_items WHERE kind='pr'`).Scan(&reviewReq)
	if !reviewReq {
		t.Error("review request from viewer not flagged")
	}

	// Second scan: commits are fetched incrementally and deduplicated;
	// fork events are not duplicated.
	second := runScan()
	if got := fake.sinceArg[app]; !got.Equal(now.AddDate(0, 0, -10)) {
		t.Errorf("incremental since = %v, want last commit - 7d", got)
	}
	var commits, stars, snaps int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM commits`).Scan(&commits)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM repo_stargazers`).Scan(&stars)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM repo_snapshots WHERE scan_id=$1`, second.ID).Scan(&snaps)
	if commits != 1 || stars != 1 || snaps != 2 {
		t.Errorf("after rescan commits=%d stars=%d snaps=%d", commits, stars, snaps)
	}
}

func TestConcurrentStartReturnsExisting(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	st := store.New(pool)
	_ = st.UpsertUser(ctx, store.User{ID: 2, NodeID: "U_2", Login: "me"})
	a, existA, err := insertScan(ctx, pool, 2, "ui")
	if err != nil || existA {
		t.Fatal(err)
	}
	b, existB, err := insertScan(ctx, pool, 2, "cron")
	if err != nil || !existB || a != b {
		t.Fatalf("second insert: id=%d existing=%v err=%v", b, existB, err)
	}
	if n, _ := RecoverStale(ctx, pool); n != 1 {
		t.Fatalf("recovered %d", n)
	}
}

func TestScanWatchedRepos(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	st := store.New(pool)
	_ = st.UpsertUser(ctx, store.User{ID: 3, NodeID: "U_3", Login: "me"})
	if _, err := pool.Exec(ctx, `INSERT INTO watched_repos (user_id, owner, name) VALUES (3, 'torvalds', 'linux')`); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	linuxRef := github.RepoRef{Owner: "torvalds", Name: "linux"}
	fake := &fakeGH{
		viewer: github.Viewer{ID: 3, NodeID: "U_3", Login: "me"},
		public: map[github.RepoRef]github.Repo{
			linuxRef: {
				ID: 2325298, Ref: linuxRef, Visibility: "public", DefaultBranch: "master",
				Stars: 190000, CreatedAt: now.AddDate(-10, 0, 0), PushedAt: &now,
				Languages: []github.LanguageBytes{{Language: "C", Bytes: 1000000}},
			},
		},
		trees:    map[github.RepoRef][]github.TreeEntry{linuxRef: {{Path: "README"}, {Path: "COPYING"}}},
		files:    map[github.FileRef]string{},
		sinceArg: map[github.RepoRef]time.Time{},
	}

	s := New(ctx, pool, st, func(context.Context, *store.User, github.Cache) (github.Client, error) {
		return fake, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }

	id, _, err := s.Start(ctx, 3, "ui")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sc, err := GetScan(ctx, pool, id)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Status == "succeeded" {
			break
		}
		if sc.Status == "failed" {
			t.Fatalf("scan failed: %s", sc.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}

	var source string
	var ghID int64
	if err := pool.QueryRow(ctx, `SELECT source, id FROM repos WHERE user_id = 3 AND full_name = 'torvalds/linux'`).Scan(&source, &ghID); err != nil {
		t.Fatal(err)
	}
	if source != "watched" || ghID != 2325298 {
		t.Fatalf("got source=%q id=%d", source, ghID)
	}
}

func TestTrendSnapshotsAndDismissal(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	st := store.New(pool)
	_ = st.UpsertUser(ctx, store.User{ID: 4, NodeID: "U_4", Login: "me"})

	day1 := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	app := github.RepoRef{Owner: "me", Name: "app"}
	fake := &fakeGH{
		viewer: github.Viewer{ID: 4, NodeID: "U_4", Login: "me"},
		repos: []github.Repo{{
			ID: 100, Ref: app, Visibility: "public", DefaultBranch: "main",
			CreatedAt: day1.AddDate(-1, 0, 0), PushedAt: &day1,
		}},
		trees:    map[github.RepoRef][]github.TreeEntry{app: {{Path: ".github/workflows/ci.yml"}}},
		files:    map[github.FileRef]string{},
		runs:     []github.WorkflowRun{{ID: 1, Name: "CI", Branch: "main", Conclusion: "failure", URL: "https://github.com/me/app/actions/runs/1", CreatedAt: day1}},
		sinceArg: map[github.RepoRef]time.Time{},
	}

	curNow := day1
	s := New(ctx, pool, st, func(context.Context, *store.User, github.Cache) (github.Client, error) {
		return fake, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return curNow }

	waitScan := func() {
		id, _, err := s.Start(ctx, 4, "ui")
		if err != nil {
			t.Fatal(err)
		}
		for range 100 {
			sc, err := GetScan(ctx, pool, id)
			if err != nil {
				t.Fatal(err)
			}
			if sc.Status == "succeeded" {
				return
			}
			if sc.Status == "failed" {
				t.Fatalf("scan failed: %s", sc.Error)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("scan timed out")
	}

	// Scan 1: ci_rot does not fire yet (only 1 scan).
	waitScan()
	var activeCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM action_items WHERE user_id = 4 AND rule = 'ci_rot' AND active = true`).Scan(&activeCount)
	if activeCount != 0 {
		t.Fatalf("expected 0 ci_rot after 1 scan, got %d", activeCount)
	}

	// Scan 2 (next day, still failing): ci_rot fires.
	curNow = day2
	waitScan()
	var itemID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM action_items WHERE user_id = 4 AND rule = 'ci_rot' AND active = true`).Scan(&itemID); err != nil {
		t.Fatalf("expected ci_rot after 2nd scan: %v", err)
	}

	// Dismiss the alert and run Scan 3: dismissed must stay true.
	if _, err := pool.Exec(ctx, `UPDATE action_items SET dismissed = true WHERE id = $1`, itemID); err != nil {
		t.Fatal(err)
	}
	curNow = day2.Add(24 * time.Hour)
	waitScan()
	var dismissed bool
	var active bool
	if err := pool.QueryRow(ctx, `SELECT dismissed, active FROM action_items WHERE id = $1`, itemID).Scan(&dismissed, &active); err != nil {
		t.Fatal(err)
	}
	if !dismissed || !active {
		t.Fatalf("expected dismissed=true active=true after rescan, got dismissed=%v active=%v", dismissed, active)
	}
}
