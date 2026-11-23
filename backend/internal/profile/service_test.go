package profile

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

type fakeRepo struct {
	text    string
	sha     string
	writes  int
	lastMsg string
}

func (f *fakeRepo) Readme(context.Context, github.RepoRef) (string, string, string, error) {
	return "README.md", f.text, f.sha, nil
}
func (f *fakeRepo) UpdateFile(_ context.Context, _ github.RepoRef, path, msg, content, sha string) (string, string, error) {
	if sha != f.sha {
		return "", "", &github.APIError{Status: 409}
	}
	f.writes++
	f.text, f.lastMsg, f.sha = content, msg, sha+"x"
	return "c0ffee", "https://github.com/me/me/commit/c0ffee", nil
}

type fakeAccess struct{ r *fakeRepo }

func (a fakeAccess) Read(context.Context, *store.User) (Reader, error) { return a.r, nil }
func (a fakeAccess) ReadmeWriter(context.Context, *store.User, int64) (Writer, error) {
	return a.r, nil
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func setup(t *testing.T) (*Service, *pgxpool.Pool, *fakeRepo) {
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
	snap := `INSERT INTO repo_snapshots (scan_id, repo_id, name, description, visibility, is_fork, is_archived, stars, forks, size_kb,
		created_at, pushed_at, has_readme, has_license, has_gitignore, has_ci, has_tests, open_prs, open_issues, health_score, health_breakdown, status)
		VALUES (1, $1, $2, $3, $4, $5, false, $6, 0, 1, now(), now() - $7::interval, true, true, true, true, true, 0, 0, 80, '[]', 'active')`
	ctx := context.Background()
	for _, q := range []string{
		`TRUNCATE users, scans, repos CASCADE`,
		`INSERT INTO users (id, node_id, login) VALUES (1, 'U1', 'me')`,
		`INSERT INTO user_settings (user_id) VALUES (1)`,
		`INSERT INTO scans (id, user_id, trigger, status, finished_at) VALUES (1, 1, 'ui', 'succeeded', now())`,
		`INSERT INTO repos (id, user_id, full_name) VALUES (1,1,'me/me'), (2,1,'me/app'), (3,1,'me/secret-plan'), (4,1,'me/old'), (5,1,'me/forked')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct {
		id         int64
		name, desc string
		vis        string
		fork       bool
		stars      int
		ago        string
	}{
		{1, "me", "profile", "public", false, 0, "1 hour"},
		{2, "app", "My app", "public", false, 5, "2 days"},
		{3, "secret-plan", "private thing", "private", false, 0, "1 hour"},
		{4, "old", "", "public", false, 0, "300 days"},
		{5, "forked", "", "public", true, 0, "1 hour"},
	} {
		if _, err := pool.Exec(ctx, snap, r.id, r.name, r.desc, r.vis, r.fork, r.stars, r.ago); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repo_languages (scan_id, repo_id, language, bytes) VALUES
		(1,2,'Go',700),(1,4,'Python',300),(1,3,'Rust',100000),(1,5,'C',100000)`); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRepo{text: "# Me\n\n" + StartMarker + "\nold\n" + EndMarker + "\nbye\n", sha: "s1"}
	s := New(pool, stats.NewService(pool), fakeAccess{fr}, "", quiet)
	return s, pool, fr
}

var me = &store.User{ID: 1, Login: "me"}

func TestPublicDataOnly(t *testing.T) {
	s, _, _ := setup(t)
	d, err := s.Data(context.Background(), me, "me/me")
	if err != nil {
		t.Fatal(err)
	}
	out := Render(d)
	if strings.Contains(out, "secret-plan") || strings.Contains(out, "Rust") {
		t.Fatalf("private repo leaked:\n%s", out)
	}
	if strings.Contains(out, "forked") || strings.Contains(out, "**C**") {
		t.Fatalf("fork included:\n%s", out)
	}
	if strings.Contains(out, "[me](") {
		t.Fatal("profile repo listed as a project")
	}
	if !strings.Contains(out, "**Go** 70% · **Python** 30%") || !strings.Contains(out, "[app](https://github.com/me/app): My app ★ 5") {
		t.Fatalf("content:\n%s", out)
	}
}

func TestApplyFlow(t *testing.T) {
	s, pool, fr := setup(t)
	ctx := context.Background()

	run, err := s.Apply(ctx, me, "cron")
	if err != nil || run.Outcome != "disabled" || fr.writes != 0 {
		t.Fatalf("disabled: %+v %v", run, err)
	}
	if _, err := s.SaveSettings(ctx, 1, Settings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	run, err = s.Apply(ctx, me, "ui")
	if err != nil || run.Outcome != "updated" || fr.writes != 1 || fr.lastMsg != commitMessage {
		t.Fatalf("update: %+v %v", run, err)
	}
	if !strings.HasPrefix(fr.text, "# Me\n\n"+StartMarker+"\n#### Top languages") || !strings.HasSuffix(fr.text, EndMarker+"\nbye\n") {
		t.Fatalf("outside text changed:\n%s", fr.text)
	}
	// Nothing changed since: no commit.
	run, _ = s.Apply(ctx, me, "cron")
	if run.Outcome != "unchanged" || fr.writes != 1 {
		t.Fatalf("unchanged: %+v writes=%d", run, fr.writes)
	}
	// Markers removed by hand: refuse, no write.
	fr.text = "# Me\nno block\n"
	run, _ = s.Apply(ctx, me, "cron")
	if run.Outcome != "no_markers" || fr.writes != 1 {
		t.Fatalf("no markers: %+v", run)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM profile_readme_runs WHERE user_id = 1`).Scan(&n)
	if n != 4 {
		t.Fatalf("runs logged = %d", n)
	}
}

func TestRepoMustBeOwnedAndScanned(t *testing.T) {
	s, pool, fr := setup(t)
	s.repoName = "someone-else/their-repo"
	_, _ = s.SaveSettings(context.Background(), 1, Settings{Enabled: true})
	if _, err := s.Apply(context.Background(), me, "ui"); err == nil || fr.writes != 0 {
		t.Fatalf("foreign repo written: %v", err)
	}
	_ = pool
}

func TestFeedOutageKeepsArticles(t *testing.T) {
	s, _, fr := setup(t)
	ctx := context.Background()
	fr.text = "x\n" + StartMarker + "\n#### Latest articles\n\n- [Old](https://medium.com/@me/old)\n" + EndMarker + "\n"
	s.feed.Transport = rt(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})
	if _, err := s.SaveSettings(ctx, 1, Settings{Enabled: true, FeedURL: "https://medium.com/feed/@me"}); err != nil {
		t.Fatal(err)
	}
	run, _ := s.Apply(ctx, me, "cron")
	if run.Outcome != "failed" || fr.writes != 0 || !strings.Contains(fr.text, "[Old]") {
		t.Fatalf("feed outage wiped articles: %+v", run)
	}
	if _, err := s.SaveSettings(ctx, 1, Settings{FeedURL: "https://evil.example/feed"}); err == nil {
		t.Fatal("bad feed URL saved")
	}
}
