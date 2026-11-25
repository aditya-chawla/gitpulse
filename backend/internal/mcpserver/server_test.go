package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/health"
	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/prreview"
	"github.com/harshit3011/gitpulse/backend/internal/releases"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
	"github.com/harshit3011/gitpulse/backend/internal/trends"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func checks(cs ...health.Check) json.RawMessage {
	b, _ := json.Marshal(cs)
	return b
}

type fake struct {
	noScan      bool
	releasesOn  bool
	created     []releases.CreateInput
	dismissed   []int64
	mergedCount int
}

var now = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

func (f *fake) User(context.Context) (*store.User, error) {
	return &store.User{ID: 1, Login: "me"}, nil
}
func (f *fake) Repos(context.Context, *store.User) (*stats.ReposResponse, error) {
	if f.noScan {
		return nil, stats.ErrNoData
	}
	full := []health.Check{{Key: "readme", Label: "README", Points: 15, Max: 15}}
	return &stats.ReposResponse{Scan: stats.ScanRef{FinishedAt: &now}, Repos: []stats.RepoRow{
		{ID: 1, Name: "good", FullName: "me/good", HealthScore: 95, Status: "active", HealthBreakdown: checks(full...)},
		{ID: 2, Name: "meh", FullName: "me/meh", HealthScore: 55, Status: "slowing",
			HealthBreakdown: checks(health.Check{Key: "license", Label: "License", Points: 0, Max: 10, Detail: "no license"})},
		{ID: 3, Name: "bad", FullName: "me/bad", HealthScore: 20, Status: "dormant", HealthBreakdown: checks()},
		{ID: 4, Name: "old", FullName: "me/old", HealthScore: 10, IsArchived: true, HealthBreakdown: checks()},
		{ID: 5, Name: "bad", FullName: "other-org/bad", HealthScore: 90, HealthBreakdown: checks()},
	}}, nil
}
func (f *fake) Repo(_ context.Context, _ *store.User, id int64) (*stats.RepoDetail, error) {
	if id != 2 {
		return nil, stats.ErrNotFound
	}
	return &stats.RepoDetail{Scan: stats.ScanRef{FinishedAt: &now},
		Repo:     stats.RepoRow{ID: 2, FullName: "me/meh", HealthScore: 55, Status: "slowing", HealthBreakdown: checks(health.Check{Key: "license", Label: "License", Max: 10})},
		Branches: []stats.BranchInfo{{Name: "main", IsDefault: true}, {Name: "old-wip", Stale: true}},
		Items:    []stats.ItemInfo{{Kind: "pr", Number: 7, Title: "Fix it", AgeDays: 40}},
		Runs:     []stats.RunInfo{{Workflow: "ci", Branch: "main"}},
	}, nil
}
func (f *fake) Analytics(context.Context, *store.User) (*stats.Analytics, error) {
	weekly := make([]stats.WeekPoint, 20)
	for i := range weekly {
		weekly[i] = stats.WeekPoint{Week: "w" + string(rune('a'+i)), Mine: i}
	}
	return &stats.Analytics{Scan: stats.ScanRef{FinishedAt: &now}, Timezone: "Asia/Kolkata", IncludedRepos: 3,
		Languages: []stats.LanguageShare{{Language: "Go", Bytes: 900, Percent: 90}, {Language: "Shell", Bytes: 100, Percent: 10}},
		Weekly:    weekly,
		Commits:   stats.CommitStats{Total: 220, BusiestWeekday: 2, BusiestHour: 22, CurrentStreak: 3, LongestStreak: stats.Streak{Days: 9}},
	}, nil
}
func (f *fake) Digest(context.Context, *store.User) (*stats.Digest, error) {
	return &stats.Digest{Scan: stats.ScanRef{FinishedAt: &now}, WaitingOnMe: []stats.ItemInfo{{Number: 3, Title: "Please review"}}}, nil
}
func (f *fake) PRReviews(context.Context, *store.User, int) ([]prreview.ReviewRow, error) {
	res, _ := json.Marshal(prreview.Result{Analysis: prreview.Analysis{FilesChanged: 4,
		Flags:           []prreview.Flag{{Kind: "migration", Title: "Database migrations or schema", Files: []string{"db/1.sql"}}},
		SuggestedLabels: []string{"bug"}}})
	return []prreview.ReviewRow{
		{Repo: "me/meh", Number: 1, Status: "done", Result: res},
		{Repo: "me/good", Number: 2, Status: "skipped", SkipReason: "PR is closed"},
	}, nil
}
func (f *fake) ReleasePreview(context.Context, *store.User, int64) (*releases.Preview, error) {
	return &releases.Preview{ReleasesEnabled: f.releasesOn, Count: f.mergedCount, SuggestedVersion: "v1.3.0",
		BaseTag: "v1.2.0", DefaultBranch: "main", Markdown: "## Features\n\n- x"}, nil
}
func (f *fake) CreateRelease(_ context.Context, _ *store.User, _ int64, in releases.CreateInput) (*github.Release, error) {
	f.created = append(f.created, in)
	return &github.Release{ID: 1, TagName: in.Tag, Draft: true, HTMLURL: "https://github.com/me/meh/releases/tag/untagged-1"}, nil
}
func (f *fake) TrendsOverview(context.Context, *store.User) ([]stats.RepoTrend, error) {
	return []stats.RepoTrend{
		{ID: 1, Name: "good", FullName: "me/good", Direction: trends.Improving},
		{ID: 2, Name: "meh", FullName: "me/meh", Direction: trends.Slipping},
	}, nil
}
func (f *fake) TrendForRepo(_ context.Context, _ *store.User, id int64) (*stats.RepoTrendDetail, error) {
	if id != 2 {
		return nil, stats.ErrNotFound
	}
	return &stats.RepoTrendDetail{
		RepoTrend: stats.RepoTrend{ID: 2, Name: "meh", FullName: "me/meh", Direction: trends.Slipping},
		Alerts:    []stats.ActionItem{{ID: 11, RepoID: 2, RepoName: "me/meh", Rule: "ci_rot", Severity: 4, Title: "CI failing"}},
	}, nil
}
func (f *fake) ActionItems(context.Context, *store.User, int) ([]stats.ActionItem, error) {
	return []stats.ActionItem{{ID: 11, RepoID: 2, RepoName: "me/meh", Rule: "ci_rot", Severity: 4, Title: "CI failing"}}, nil
}
func (f *fake) DismissActionItem(_ context.Context, _ *store.User, id int64) error {
	if id != 11 {
		return stats.ErrNotFound
	}
	f.dismissed = append(f.dismissed, id)
	return nil
}
func (f *fake) Insights(_ context.Context, _ *store.User, source string) ([]insights.RepoInsights, error) {
	all := []insights.RepoInsights{
		{RepoID: 1, FullName: "me/good", Source: "owned", HealthScore: 95, ContributorReadiness: 90, ReadinessVerdict: "Welcoming"},
		{RepoID: 2, FullName: "me/meh", Source: "owned", HealthScore: 55, ContributorReadiness: 60, ReadinessVerdict: "Steady"},
		{RepoID: 6, FullName: "oss/cool", Source: "watched", HealthScore: 84, ContributorReadiness: 86, ReadinessVerdict: "Welcoming"},
	}
	if source == "" {
		return all, nil
	}
	var out []insights.RepoInsights
	for _, r := range all {
		if r.Source == source {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fake) RepoInsights(_ context.Context, _ *store.User, id int64) (*insights.RepoInsights, error) {
	if id != 2 {
		return nil, stats.ErrNotFound
	}
	return &insights.RepoInsights{RepoID: 2, FullName: "me/meh", Source: "owned", HealthScore: 55, ContributorReadiness: 60}, nil
}

func connect(t *testing.T, f *fake, writes bool) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	srv := NewServer(f, writes, quiet)
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call runs a tool and decodes its JSON text result. isErr reports tool errors.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (out map[string]any, errText string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	txt := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		return nil, txt
	}
	if err := json.Unmarshal([]byte(txt), &out); err != nil {
		t.Fatalf("%s: bad JSON %q", name, txt)
	}
	return out, ""
}

func TestToolList(t *testing.T) {
	names := func(writes bool) []string {
		res, err := connect(t, &fake{}, writes).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, tl := range res.Tools {
			n = append(n, tl.Name)
			if tl.Name != "draft_release_notes" && tl.Name != "dismiss_action" && (tl.Annotations == nil || !tl.Annotations.ReadOnlyHint) {
				t.Errorf("%s must be annotated read-only", tl.Name)
			}
		}
		slices.Sort(n)
		return n
	}
	ro := []string{"compare_watched_repos", "get_action_items", "get_activity", "get_digest", "get_language_stats", "get_pr_reviews", "get_repo_health", "get_repo_insights", "get_trends", "list_repos_needing_attention"}
	if got := names(false); !slices.Equal(got, ro) {
		t.Fatalf("read-only tools = %v", got)
	}
	if got := names(true); !slices.Contains(got, "draft_release_notes") || !slices.Contains(got, "dismiss_action") || len(got) != 12 {
		t.Fatalf("with writes = %v", got)
	}
}

func TestListReposNeedingAttention(t *testing.T) {
	cs := connect(t, &fake{}, false)
	out, _ := call(t, cs, "list_repos_needing_attention", nil)
	repos := out["repos"].([]any)
	if len(repos) != 2 || repos[0].(map[string]any)["repo"] != "me/bad" || repos[1].(map[string]any)["repo"] != "me/meh" {
		t.Fatalf("repos = %v", repos)
	}
	if missing := repos[1].(map[string]any)["missing"].([]any); len(missing) != 1 || !strings.Contains(missing[0].(string), "License (0/10)") {
		t.Fatalf("missing = %v", missing)
	}
	out, _ = call(t, cs, "list_repos_needing_attention", map[string]any{"includeArchived": true, "limit": 1})
	if out["matching"].(float64) != 3 || len(out["repos"].([]any)) != 1 || out["repos"].([]any)[0].(map[string]any)["repo"] != "me/old" {
		t.Fatalf("archived+limit = %v", out)
	}
	if _, e := call(t, connect(t, &fake{noScan: true}, false), "list_repos_needing_attention", nil); !strings.Contains(e, "run a scan") {
		t.Fatalf("no scan error = %q", e)
	}
}

func TestGetRepoHealth(t *testing.T) {
	cs := connect(t, &fake{}, false)
	out, e := call(t, cs, "get_repo_health", map[string]any{"repo": "MEH"})
	if e != "" || out["repo"] != "me/meh" || out["score"].(float64) != 55 {
		t.Fatalf("out=%v err=%q", out, e)
	}
	if sb := out["staleBranches"].([]any); len(sb) != 1 || sb[0] != "old-wip" {
		t.Fatalf("stale = %v", sb)
	}
	if _, e := call(t, cs, "get_repo_health", map[string]any{"repo": "bad"}); !strings.Contains(e, "several repositories") {
		t.Fatalf("ambiguous: %q", e)
	}
	if _, e := call(t, cs, "get_repo_health", map[string]any{"repo": "nope"}); !strings.Contains(e, "no repository") {
		t.Fatalf("unknown: %q", e)
	}
	if _, e := call(t, cs, "get_repo_health", map[string]any{}); e == "" {
		t.Fatal("missing repo accepted")
	}
}

func TestLanguageActivityDigestPRs(t *testing.T) {
	cs := connect(t, &fake{}, false)
	out, _ := call(t, cs, "get_language_stats", nil)
	if l := out["languages"].([]any); len(l) != 2 || l[0].(map[string]any)["language"] != "Go" {
		t.Fatalf("languages = %v", out)
	}
	out, _ = call(t, cs, "get_activity", map[string]any{"weeks": 4})
	if w := out["weekly"].([]any); len(w) != 4 || w[3].(map[string]any)["mine"].(float64) != 19 {
		t.Fatalf("weekly = %v", out["weekly"])
	}
	if out["busiestWeekday"] != "Tuesday" || out["commitsLast52Weeks"].(float64) != 220 {
		t.Fatalf("activity = %v", out)
	}
	out, _ = call(t, cs, "get_digest", nil)
	if w := out["waitingOnMe"].([]any); len(w) != 1 {
		t.Fatalf("digest = %v", out)
	}
	out, _ = call(t, cs, "get_pr_reviews", map[string]any{"repo": "meh"})
	rv := out["reviews"].([]any)
	if len(rv) != 1 || rv[0].(map[string]any)["filesChanged"].(float64) != 4 ||
		!strings.Contains(rv[0].(map[string]any)["notable"].([]any)[0].(string), "db/1.sql") {
		t.Fatalf("reviews = %v", rv)
	}
}

func TestDraftReleaseNotes(t *testing.T) {
	f := &fake{mergedCount: 3}
	cs := connect(t, f, true)
	if _, e := call(t, cs, "draft_release_notes", map[string]any{"repo": "meh"}); !strings.Contains(e, "turned off") || len(f.created) != 0 {
		t.Fatalf("toggle off: %q created=%d", e, len(f.created))
	}
	f.releasesOn = true
	out, e := call(t, cs, "draft_release_notes", map[string]any{"repo": "meh"})
	if e != "" || out["draft"] != true || out["tag"] != "v1.3.0" || len(f.created) != 1 || f.created[0].Body == "" {
		t.Fatalf("create: %v %q", out, e)
	}
	_, _ = call(t, cs, "draft_release_notes", map[string]any{"repo": "meh", "version": "v2.0.0"})
	if f.created[1].Tag != "v2.0.0" {
		t.Fatalf("explicit version ignored: %+v", f.created[1])
	}
	f.mergedCount = 0
	if _, e := call(t, cs, "draft_release_notes", map[string]any{"repo": "meh"}); !strings.Contains(e, "no release needed") {
		t.Fatalf("nothing merged: %q", e)
	}
}

func TestHTTPAuth(t *testing.T) {
	const tok = "0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(Handler(NewServer(&fake{}, false, quiet), tok, quiet))
	defer srv.Close()
	post := func(auth string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post(""); c != http.StatusUnauthorized {
		t.Errorf("no token: %d", c)
	}
	if c := post("Bearer wrong"); c != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", c)
	}
	if c := post("Basic " + tok); c != http.StatusUnauthorized {
		t.Errorf("non-bearer: %d", c)
	}

	// A real client over streamable HTTP with the right token.
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+tok)
		return http.DefaultTransport.RoundTrip(r)
	})}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: hc, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(res.Tools) != 10 {
		t.Fatalf("tools over HTTP: %v %v", res, err)
	}

	off := httptest.NewServer(Handler(NewServer(&fake{}, false, quiet), "", quiet))
	defer off.Close()
	resp, _ := http.Post(off.URL, "application/json", strings.NewReader("{}"))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no MCP_TOKEN: %d", resp.StatusCode)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTrendAndActionTools(t *testing.T) {
	f := &fake{}
	cs := connect(t, f, true)

	out, errText := call(t, cs, "get_trends", nil)
	if errText != "" || len(out["repos"].([]any)) != 2 {
		t.Fatalf("get_trends all: out=%v err=%q", out, errText)
	}

	out, errText = call(t, cs, "get_trends", map[string]any{"repo": "me/meh"})
	if errText != "" || out["direction"] != "slipping" {
		t.Fatalf("get_trends single: out=%v err=%q", out, errText)
	}

	out, errText = call(t, cs, "get_action_items", map[string]any{"limit": 5})
	if errText != "" || len(out["items"].([]any)) != 1 {
		t.Fatalf("get_action_items: out=%v err=%q", out, errText)
	}

	out, errText = call(t, cs, "dismiss_action", map[string]any{"id": 11})
	if errText != "" || out["ok"] != true || len(f.dismissed) != 1 {
		t.Fatalf("dismiss_action: out=%v err=%q dismissed=%v", out, errText, f.dismissed)
	}
}
