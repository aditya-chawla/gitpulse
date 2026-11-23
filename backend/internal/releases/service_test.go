package releases

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

type fakeGH struct {
	latest   *github.Release
	tags     []github.Tag
	dates    map[string]time.Time
	merged   []github.MergedPR
	gotSince time.Time
	gotBase  string
}

func (f *fakeGH) Repo(context.Context, github.RepoRef) (*github.RepoInfo, error) {
	return &github.RepoInfo{ID: 9, FullName: "me/sandbox", DefaultBranch: "main"}, nil
}
func (f *fakeGH) LatestRelease(context.Context, github.RepoRef) (*github.Release, error) {
	return f.latest, nil
}
func (f *fakeGH) Tags(context.Context, github.RepoRef) ([]github.Tag, error) { return f.tags, nil }
func (f *fakeGH) CommitDate(_ context.Context, _ github.RepoRef, sha string) (time.Time, error) {
	return f.dates[sha], nil
}
func (f *fakeGH) MergedPRs(_ context.Context, _ github.RepoRef, base string, since time.Time, _ int) ([]github.MergedPR, error) {
	f.gotSince, f.gotBase = since, base
	return f.merged, nil
}

type fakeWriter struct {
	calls             int
	tag, target, name string
	body              string
}

func (w *fakeWriter) CreateDraftRelease(_ context.Context, _ github.RepoRef, tag, target, name, body string) (*github.Release, error) {
	w.calls++
	w.tag, w.target, w.name, w.body = tag, target, name, body
	return &github.Release{ID: 42, TagName: tag, Draft: true, HTMLURL: "https://github.com/me/sandbox/releases/tag/untagged-x"}, nil
}

type fakeAccess struct {
	gh *fakeGH
	w  *fakeWriter
}

func (a fakeAccess) Read(context.Context, *store.User) (GitHub, error) { return a.gh, nil }
func (a fakeAccess) ReleaseWriter(context.Context, *store.User, int64) (Writer, error) {
	return a.w, nil
}

type fakeLLM struct{ reply string }

func (fakeLLM) Name() string  { return "fake" }
func (fakeLLM) Model() string { return "fake" }
func (f fakeLLM) Complete(context.Context, llm.ProviderRequest) (llm.ProviderResponse, error) {
	return llm.ProviderResponse{Text: f.reply, InputTokens: 10, OutputTokens: 10}, nil
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func setup(t *testing.T, p llm.Provider) (*Service, *pgxpool.Pool, *fakeGH, *fakeWriter) {
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
	for _, q := range []string{
		`TRUNCATE users, repos, release_drafts, llm_cache CASCADE`,
		`INSERT INTO users (id, node_id, login) VALUES (1, 'U1', 'me')`,
		`INSERT INTO repos (id, user_id, full_name) VALUES (9, 1, 'me/sandbox')`,
	} {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	gh := &fakeGH{dates: map[string]time.Time{}}
	w := &fakeWriter{}
	ai := llm.New(p, pool, llm.Config{MonthlyBudgetUSD: 5, Price: llm.Price{InputPerMTok: 1, OutputPerMTok: 1}}, quiet)
	return New(pool, store.New(pool), fakeAccess{gh, w}, ai, quiet), pool, gh, w
}

var me = &store.User{ID: 1, Login: "me"}

func TestCreateRequiresToggleAndIsDraft(t *testing.T) {
	s, pool, _, w := setup(t, nil)
	ctx := context.Background()
	in := CreateInput{Tag: "v0.2.0", Body: "notes"}

	if _, err := s.Create(ctx, me, 9, in, "ui"); !errors.Is(err, ErrNotAllowed) || w.calls != 0 {
		t.Fatalf("toggle off: err=%v calls=%d", err, w.calls)
	}
	if _, err := store.New(pool).SaveRepoSetting(ctx, 1, store.RepoSetting{RepoID: 9, PRMode: "dashboard", ReleasesEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, me, 9, CreateInput{Tag: "v1 .0"}, "ui"); !errors.Is(err, ErrInvalid) || w.calls != 0 {
		t.Fatalf("bad tag: %v", err)
	}
	if _, err := s.Create(ctx, me, 1234, in, "ui"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign repo: %v", err)
	}
	rel, err := s.Create(ctx, me, 9, in, "ui")
	if err != nil || !rel.Draft || w.calls != 1 || w.name != "v0.2.0" {
		t.Fatalf("create: %+v %v calls=%d name=%q", rel, err, w.calls, w.name)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM release_drafts WHERE tag = 'v0.2.0' AND github_release_id = 42`).Scan(&n)
	if n != 1 {
		t.Fatal("draft not recorded")
	}
}

func TestPreviewBaseFromReleaseThenTag(t *testing.T) {
	s, _, gh, _ := setup(t, nil)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	// No release: highest semver tag, cut-off at its commit date.
	gh.tags = []github.Tag{{Name: "v0.9.0", SHA: "a"}, {Name: "v0.10.0", SHA: "b"}, {Name: "nightly", SHA: "c"}}
	gh.dates["b"] = t0
	gh.merged = []github.MergedPR{mpr(3, "feat: new thing", "me")}
	p, err := s.Preview(ctx, me, 9)
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseTag != "v0.10.0" || p.BaseSource != "tag" || !gh.gotSince.Equal(t0) || gh.gotBase != "main" {
		t.Fatalf("tag base: %+v since=%v", p, gh.gotSince)
	}
	if p.SuggestedVersion != "v0.11.0" || p.AIStatus != "disabled" || !strings.Contains(p.Markdown, "feat: new thing by @me") {
		t.Fatalf("preview: %+v", p)
	}

	// A published release wins over a higher tag.
	gh.latest = &github.Release{TagName: "v0.9.0"}
	gh.dates["a"] = t0.Add(-time.Hour)
	p, _ = s.Preview(ctx, me, 9)
	if p.BaseTag != "v0.9.0" || p.BaseSource != "release" {
		t.Fatalf("release base: %+v", p)
	}

	// Nothing to compare against: everything merged, v0.1.0.
	gh.latest, gh.tags = nil, nil
	p, _ = s.Preview(ctx, me, 9)
	if p.BaseTag != "" || !gh.gotSince.IsZero() || p.SuggestedVersion != "v0.1.0" || p.CompareURL != "" {
		t.Fatalf("no base: %+v", p)
	}
}

func TestPreviewAISummariesAreSanitised(t *testing.T) {
	reply := `{"intro":"Big release! See [docs](https://evil.example) @everyone","items":[{"number":3,"summary":"Added <script>x</script> export"},{"number":999,"summary":"invented"}]}`
	s, _, gh, _ := setup(t, fakeLLM{reply: reply})
	gh.merged = []github.MergedPR{mpr(3, "feat: export", "me")}
	p, err := s.Preview(context.Background(), me, 9)
	if err != nil || p.AIStatus != "ok" {
		t.Fatalf("%v %s", err, p.AIStatus)
	}
	if strings.Contains(p.Intro, "evil.example") || strings.Contains(p.Intro, "@everyone") {
		t.Errorf("intro not sanitised: %q", p.Intro)
	}
	if it := p.Groups[0].Items[0]; it.Summary != "Added x export" {
		t.Errorf("summary = %q", it.Summary)
	}
	if strings.Contains(p.Markdown, "invented") {
		t.Error("summary for an unknown PR number was used")
	}
}
