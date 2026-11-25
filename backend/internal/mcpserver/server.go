// Package mcpserver exposes gitpulse data as MCP tools over streamable HTTP
// (mounted at /mcp, bearer-token protected). Every tool is read-only except
// draft_release_notes, which exists only when writes are allowed and can
// only create a DRAFT release in a repo whose "Draft releases" switch is on.
package mcpserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
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

// Backend is the data the tools read. Live adapts the real services; tests
// use a fake.
type Backend interface {
	User(ctx context.Context) (*store.User, error)
	Repos(ctx context.Context, u *store.User) (*stats.ReposResponse, error)
	Repo(ctx context.Context, u *store.User, repoID int64) (*stats.RepoDetail, error)
	Analytics(ctx context.Context, u *store.User) (*stats.Analytics, error)
	Digest(ctx context.Context, u *store.User) (*stats.Digest, error)
	PRReviews(ctx context.Context, u *store.User, limit int) ([]prreview.ReviewRow, error)
	ReleasePreview(ctx context.Context, u *store.User, repoID int64) (*releases.Preview, error)
	CreateRelease(ctx context.Context, u *store.User, repoID int64, in releases.CreateInput) (*github.Release, error)
	TrendsOverview(ctx context.Context, u *store.User) ([]stats.RepoTrend, error)
	TrendForRepo(ctx context.Context, u *store.User, repoID int64) (*stats.RepoTrendDetail, error)
	ActionItems(ctx context.Context, u *store.User, limit int) ([]stats.ActionItem, error)
	DismissActionItem(ctx context.Context, u *store.User, id int64) error
	Insights(ctx context.Context, u *store.User, source string) ([]insights.RepoInsights, error)
	RepoInsights(ctx context.Context, u *store.User, repoID int64) (*insights.RepoInsights, error)
}

const Version = "1.0.0"

// NewServer registers the tools. draft_release_notes is registered only
// when allowWrites is true, so clients never even see it otherwise.
func NewServer(b Backend, allowWrites bool, log *slog.Logger) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gitpulse", Title: "gitpulse", Version: Version}, &mcp.ServerOptions{
		Instructions: "gitpulse is a repository intelligence and maintenance dashboard for the user's own and watched public GitHub repositories, built from a daily scan. " +
			"Use these tools to answer questions about repo health, 7d/30d trends (improving/steady/slipping), prioritized maintenance actions, languages, commit activity, and pull request reviews. " +
			"Data is as of the latest scan (see scannedAt in results).",
	})
	t := &tools{b: b, log: log}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)}

	mcp.AddTool(s, &mcp.Tool{Name: "list_repos_needing_attention", Annotations: ro,
		Description: "List the user's repositories with the lowest health scores, worst first, with the checks each one is missing (README, license, CI, tests, recent activity, stale branches, old PRs, description/topics)."},
		t.reposNeedingAttention)
	mcp.AddTool(s, &mcp.Tool{Name: "get_repo_health", Annotations: ro,
		Description: "Health score (0-100) and full breakdown for one repository, plus its open PRs and issues, stale branches and recent failed workflow runs."},
		t.repoHealth)
	mcp.AddTool(s, &mcp.Tool{Name: "get_language_stats", Annotations: ro,
		Description: "Language mix across the user's repositories by bytes, and how it changed by year."},
		t.languageStats)
	mcp.AddTool(s, &mcp.Tool{Name: "get_activity", Annotations: ro,
		Description: "The user's commit activity: weekly commit counts, streaks, busiest weekday and hour (in the user's timezone), and the repos with the most commits."},
		t.activity)
	mcp.AddTool(s, &mcp.Tool{Name: "get_digest", Annotations: ro,
		Description: "What changed since the previous scan: PRs waiting on the user, new issues, failed workflow runs, new forks and star changes."},
		t.digest)
	mcp.AddTool(s, &mcp.Tool{Name: "get_pr_reviews", Annotations: ro,
		Description: "Recent gitpulse PR assistant reviews: notable/risky changes, suggested labels and (when AI is on) summaries. Optionally filter by repository."},
		t.prReviews)
	mcp.AddTool(s, &mcp.Tool{Name: "get_trends", Annotations: ro,
		Description: "7-day and 30-day trend direction (improving, steady, slipping) and metric deltas across all tracked repositories, or full trend history and decay alerts for one repository."},
		t.trends)
	mcp.AddTool(s, &mcp.Tool{Name: "get_action_items", Annotations: ro,
		Description: "Prioritized, severity-weighted 'What to Fix Next' maintenance action queue across owned and watched repositories."},
		t.actionItems)
	mcp.AddTool(s, &mcp.Tool{Name: "get_repo_insights", Annotations: ro,
		Description: "Postgres-computed data science insights: Z-score anomalies, PR age median/P90 percentiles, 30-day health trajectory linear regression slope, contributor diversity, and contributor readiness. Filter by repo or source ('owned' or 'watched')."},
		t.repoInsights)
	mcp.AddTool(s, &mcp.Tool{Name: "compare_watched_repos", Annotations: ro,
		Description: "Compare all watched open-source repositories ranked by contributor readiness, showing health trajectory, PR responsiveness, diversity score, and statistical anomalies."},
		t.compareWatched)
	if allowWrites {
		mcp.AddTool(s, &mcp.Tool{Name: "draft_release_notes",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: false, OpenWorldHint: new(true)},
			Description: "Create a DRAFT GitHub release for a repository with notes for everything merged since the last release, grouped into features, fixes, internal and dependencies. " +
				"It never publishes. The repository's 'Draft releases' switch must be on in gitpulse Settings. Omit version to use the suggested semver version."},
			t.draftRelease)
		mcp.AddTool(s, &mcp.Tool{Name: "dismiss_action",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false)},
			Description: "Dismiss a prioritized maintenance action item by ID so it stays hidden across future scans."},
			t.dismissAction)
	}
	return s
}

type tools struct {
	b   Backend
	log *slog.Logger
}

// text returns a JSON result as text content (and as structured content).
func text(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// friendly turns expected errors into messages a model can act on.
func friendly(err error) error {
	switch {
	case errors.Is(err, stats.ErrNoData):
		return errors.New("no scan has finished yet: run a scan in gitpulse first")
	case errors.Is(err, stats.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return errors.New("repository not found in the latest scan")
	}
	return err
}

func (t *tools) user(ctx context.Context) (*store.User, error) {
	u, err := t.b.User(ctx)
	if err != nil {
		return nil, fmt.Errorf("gitpulse user unavailable: %w", err)
	}
	return u, nil
}

// findRepo resolves "name" or "owner/name" (case-insensitive) in the latest scan.
func (t *tools) findRepo(ctx context.Context, u *store.User, name string) (*stats.RepoRow, error) {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(name, "/"), "https://github.com/"))
	if name == "" {
		return nil, errors.New("repo is required")
	}
	rs, err := t.b.Repos(ctx, u)
	if err != nil {
		return nil, friendly(err)
	}
	var matches []stats.RepoRow
	for _, r := range rs.Repos {
		if strings.EqualFold(r.FullName, name) || strings.EqualFold(r.Name, name) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, fmt.Errorf("no repository named %q in the latest scan", name)
	default:
		return nil, fmt.Errorf("%q matches several repositories; use owner/name", name)
	}
}

func checksOf(r stats.RepoRow) []health.Check {
	var c []health.Check
	_ = json.Unmarshal(r.HealthBreakdown, &c)
	return c
}

// --- list_repos_needing_attention ---

type AttentionIn struct {
	Limit           int  `json:"limit,omitempty" jsonschema:"maximum repositories to return (default 10, max 50)"`
	MaxScore        int  `json:"maxScore,omitempty" jsonschema:"only repositories scoring below this (default 70)"`
	IncludeArchived bool `json:"includeArchived,omitempty" jsonschema:"include archived repositories and forks (default false)"`
}

type attentionRepo struct {
	Repo      string           `json:"repo"`
	Score     int              `json:"score"`
	Status    string           `json:"status"`
	Direction trends.Direction `json:"direction,omitempty"`
	PushedAt  *time.Time       `json:"pushedAt,omitempty"`
	Missing   []string         `json:"missing"`
	Alerts    []string         `json:"alerts,omitempty"`
	URL       string           `json:"url"`
}

func (t *tools) reposNeedingAttention(ctx context.Context, _ *mcp.CallToolRequest, in AttentionIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	rs, err := t.b.Repos(ctx, u)
	if err != nil {
		return nil, nil, friendly(err)
	}
	alertsByRepo := map[int64][]string{}
	if items, err := t.b.ActionItems(ctx, u, 50); err == nil {
		for _, it := range items {
			alertsByRepo[it.RepoID] = append(alertsByRepo[it.RepoID], it.Title)
		}
	}
	limit, maxScore := clamp(in.Limit, 10, 1, 50), clamp(in.MaxScore, 70, 1, 101)
	var out []attentionRepo
	for _, r := range rs.Repos {
		if (!in.IncludeArchived && (r.IsArchived || r.IsFork)) || (r.HealthScore >= maxScore && len(alertsByRepo[r.ID]) == 0) {
			continue
		}
		a := attentionRepo{Repo: r.FullName, Score: r.HealthScore, Status: r.Status, Direction: r.Direction, PushedAt: r.PushedAt,
			URL: "https://github.com/" + r.FullName, Missing: []string{}, Alerts: alertsByRepo[r.ID]}
		for _, c := range checksOf(r) {
			if c.Points < c.Max {
				a.Missing = append(a.Missing, fmt.Sprintf("%s (%d/%d): %s", c.Label, c.Points, c.Max, c.Detail))
			}
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(a, b attentionRepo) int { return a.Score - b.Score })
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []attentionRepo{}
	}
	return text(map[string]any{"scannedAt": rs.Scan.FinishedAt, "matching": total, "repos": out})
}

func clamp(v, def, lo, hi int) int {
	if v == 0 {
		return def
	}
	return max(lo, min(hi, v))
}

// --- get_repo_health ---

type RepoIn struct {
	Repo string `json:"repo" jsonschema:"repository name, e.g. gitpulse or owner/gitpulse"`
}

func (t *tools) repoHealth(ctx context.Context, _ *mcp.CallToolRequest, in RepoIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	r, err := t.findRepo(ctx, u, in.Repo)
	if err != nil {
		return nil, nil, err
	}
	if r.Source == "watched" {
		return nil, nil, errors.New("watched repositories are read-only; draft releases cannot be created")
	}
	d, err := t.b.Repo(ctx, u, r.ID)
	if err != nil {
		return nil, nil, friendly(err)
	}
	var stale []string
	for _, b := range d.Branches {
		if b.Stale {
			stale = append(stale, b.Name)
		}
	}
	type item struct {
		Kind    string `json:"kind"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Author  string `json:"author"`
		AgeDays int    `json:"ageDays"`
		URL     string `json:"url"`
	}
	items := []item{}
	for _, it := range d.Items {
		items = append(items, item{it.Kind, it.Number, it.Title, it.Author, it.AgeDays, it.URL})
	}
	return text(map[string]any{
		"scannedAt": d.Scan.FinishedAt, "repo": d.Repo.FullName, "url": "https://github.com/" + d.Repo.FullName,
		"description": d.Repo.Description, "visibility": d.Repo.Visibility, "archived": d.Repo.IsArchived,
		"score": d.Repo.HealthScore, "status": d.Repo.Status, "checks": checksOf(d.Repo),
		"trend":    d.Trend,
		"pushedAt": d.Repo.PushedAt, "stars": d.Repo.Stars, "forks": d.Repo.Forks,
		"openPRsAndIssues": items, "staleBranches": nonNil(stale), "failedRuns": nonNilRuns(d.Runs),
		"primaryLanguage": d.Repo.PrimaryLanguage,
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilRuns(r []stats.RunInfo) []stats.RunInfo {
	if r == nil {
		return []stats.RunInfo{}
	}
	return r
}

// --- get_language_stats ---

type NoArgs struct{}

func (t *tools) languageStats(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	a, err := t.b.Analytics(ctx, u)
	if err != nil {
		return nil, nil, friendly(err)
	}
	return text(map[string]any{
		"scannedAt": a.Scan.FinishedAt, "includedRepos": a.IncludedRepos,
		"languages": a.Languages, "byYear": a.Trend,
		"note": "percent is of total bytes across included repositories; byYear attributes each repo's bytes to every year from creation to last push",
	})
}

// --- get_activity ---

type ActivityIn struct {
	Weeks int `json:"weeks,omitempty" jsonschema:"how many recent weeks of weekly counts to return (default 12, max 52)"`
}

var weekdays = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func (t *tools) activity(ctx context.Context, _ *mcp.CallToolRequest, in ActivityIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	a, err := t.b.Analytics(ctx, u)
	if err != nil {
		return nil, nil, friendly(err)
	}
	n := clamp(in.Weeks, 12, 1, 52)
	weekly := a.Weekly
	if len(weekly) > n {
		weekly = weekly[len(weekly)-n:]
	}
	c := a.Commits
	return text(map[string]any{
		"scannedAt": a.Scan.FinishedAt, "timezone": a.Timezone,
		"commitsLast52Weeks": c.Total, "activeDays": c.ActiveDays,
		"longestStreak": c.LongestStreak, "currentStreakDays": c.CurrentStreak,
		"busiestWeekday": weekdays[c.BusiestWeekday%7], "busiestHour": c.BusiestHour,
		"weekly":            weekly,
		"weeklyNote":        "mine = the user's default-branch commits; all = all authors (GitHub commit activity)",
		"topReposByCommits": a.TopByCommits,
	})
}

// --- get_digest ---

func (t *tools) digest(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	d, err := t.b.Digest(ctx, u)
	if err != nil {
		return nil, nil, friendly(err)
	}
	return text(d)
}

// --- get_pr_reviews ---

type PRReviewsIn struct {
	Repo  string `json:"repo,omitempty" jsonschema:"only reviews for this repository (name or owner/name)"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum reviews to return (default 20, max 100)"`
}

type prReview struct {
	Repo            string    `json:"repo"`
	Number          int       `json:"number"`
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	HeadSHA         string    `json:"headSha"`
	Status          string    `json:"status"`
	SkipReason      string    `json:"skipReason,omitempty"`
	FilesChanged    int       `json:"filesChanged,omitempty"`
	Notable         []string  `json:"notable,omitempty"`
	SuggestedLabels []string  `json:"suggestedLabels,omitempty"`
	AISummary       string    `json:"aiSummary,omitempty"`
	ReviewedAt      time.Time `json:"reviewedAt"`
}

func (t *tools) prReviews(ctx context.Context, _ *mcp.CallToolRequest, in PRReviewsIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	limit := clamp(in.Limit, 20, 1, 100)
	rows, err := t.b.PRReviews(ctx, u, 500)
	if err != nil {
		return nil, nil, err
	}
	want := strings.ToLower(strings.TrimSpace(in.Repo))
	out := []prReview{}
	for _, r := range rows {
		if want != "" && strings.ToLower(r.Repo) != want && !strings.HasSuffix(strings.ToLower(r.Repo), "/"+want) {
			continue
		}
		p := prReview{Repo: r.Repo, Number: r.Number, Title: r.Title, URL: r.URL, HeadSHA: r.HeadSHA,
			Status: r.Status, SkipReason: r.SkipReason, ReviewedAt: r.UpdatedAt}
		if len(r.Result) > 0 {
			var res prreview.Result
			if json.Unmarshal(r.Result, &res) == nil {
				p.FilesChanged = res.FilesChanged
				for _, f := range res.Flags {
					p.Notable = append(p.Notable, f.Title+": "+strings.Join(f.Files, ", "))
				}
				p.SuggestedLabels = res.SuggestedLabels
				if res.AI != nil {
					p.AISummary = res.AI.Summary
				}
			}
		}
		out = append(out, p)
		if len(out) == limit {
			break
		}
	}
	return text(map[string]any{"reviews": out})
}

// --- draft_release_notes (write; registered only with MCP_ALLOW_WRITES) ---

type DraftIn struct {
	Repo    string `json:"repo" jsonschema:"repository name or owner/name"`
	Version string `json:"version,omitempty" jsonschema:"tag for the draft, e.g. v1.4.0; default is the suggested next semver version"`
}

func (t *tools) draftRelease(ctx context.Context, _ *mcp.CallToolRequest, in DraftIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	r, err := t.findRepo(ctx, u, in.Repo)
	if err != nil {
		return nil, nil, err
	}
	if r.Source == "watched" {
		return nil, nil, errors.New("watched repositories are read-only; draft releases cannot be created")
	}
	p, err := t.b.ReleasePreview(ctx, u, r.ID)
	if err != nil {
		return nil, nil, friendly(err)
	}
	if !p.ReleasesEnabled {
		return nil, nil, releases.ErrNotAllowed
	}
	if p.Count == 0 {
		return nil, nil, fmt.Errorf("nothing merged into %s since %s: no release needed", p.DefaultBranch, p.BaseTag)
	}
	tag := strings.TrimSpace(in.Version)
	if tag == "" {
		tag = p.SuggestedVersion
	}
	rel, err := t.b.CreateRelease(ctx, u, r.ID, releases.CreateInput{Tag: tag, Name: tag, Body: p.Markdown, Target: p.DefaultBranch, BaseTag: p.BaseTag})
	if err != nil {
		return nil, nil, friendly(err)
	}
	t.log.Info("mcp draft release", "repo", r.FullName, "tag", tag)
	return text(map[string]any{"draft": true, "tag": tag, "url": rel.HTMLURL, "mergedPRs": p.Count,
		"baseTag": p.BaseTag, "notes": p.Markdown, "note": "Created as a draft; review and publish it on GitHub."})
}

// --- get_trends ---

type TrendsIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"optional repository name or owner/name; omit to list all tracked repositories"`
}

func (t *tools) trends(ctx context.Context, _ *mcp.CallToolRequest, in TrendsIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Repo) != "" {
		r, err := t.findRepo(ctx, u, in.Repo)
		if err != nil {
			return nil, nil, err
		}
		d, err := t.b.TrendForRepo(ctx, u, r.ID)
		if err != nil {
			return nil, nil, friendly(err)
		}
		return text(d)
	}
	repos, err := t.b.TrendsOverview(ctx, u)
	if err != nil {
		return nil, nil, friendly(err)
	}
	return text(map[string]any{"repos": repos})
}

// --- get_action_items ---

type ActionItemsIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum action items to return (default 10, max 50)"`
}

func (t *tools) actionItems(ctx context.Context, _ *mcp.CallToolRequest, in ActionItemsIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := t.b.ActionItems(ctx, u, clamp(in.Limit, 10, 1, 50))
	if err != nil {
		return nil, nil, friendly(err)
	}
	return text(map[string]any{"items": items})
}

// --- get_repo_insights ---

type InsightsIn struct {
	Repo   string `json:"repo,omitempty" jsonschema:"optional repository name (owner/name or short name)"`
	Source string `json:"source,omitempty" jsonschema:"optional source filter: 'owned' or 'watched'"`
}

func (t *tools) repoInsights(ctx context.Context, _ *mcp.CallToolRequest, in InsightsIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Repo) != "" {
		r, err := t.findRepo(ctx, u, in.Repo)
		if err != nil {
			return nil, nil, err
		}
		res, err := t.b.RepoInsights(ctx, u, r.ID)
		if err != nil {
			return nil, nil, friendly(err)
		}
		return text(res)
	}
	items, err := t.b.Insights(ctx, u, in.Source)
	if err != nil {
		return nil, nil, friendly(err)
	}
	return text(map[string]any{"repos": items})
}

// --- compare_watched_repos ---

type CompareWatchedIn struct{}

func (t *tools) compareWatched(ctx context.Context, _ *mcp.CallToolRequest, _ CompareWatchedIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := t.b.Insights(ctx, u, "watched")
	if err != nil {
		return nil, nil, friendly(err)
	}
	slices.SortStableFunc(items, func(a, b insights.RepoInsights) int {
		if a.ContributorReadiness != b.ContributorReadiness {
			return b.ContributorReadiness - a.ContributorReadiness
		}
		return b.HealthScore - a.HealthScore
	})
	return text(map[string]any{"watchedRepos": items})
}

// --- dismiss_action (write; registered only with MCP_ALLOW_WRITES) ---

type DismissActionIn struct {
	ID int64 `json:"id" jsonschema:"action item ID to dismiss"`
}

func (t *tools) dismissAction(ctx context.Context, _ *mcp.CallToolRequest, in DismissActionIn) (*mcp.CallToolResult, any, error) {
	u, err := t.user(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := t.b.DismissActionItem(ctx, u, in.ID); err != nil {
		if errors.Is(err, stats.ErrNotFound) {
			return nil, nil, errors.New("action item not found")
		}
		return nil, nil, err
	}
	return text(map[string]any{"ok": true, "id": in.ID})
}

// --- HTTP ---

// Handler serves MCP over streamable HTTP. Requests need
// "Authorization: Bearer <token>"; an empty token disables the endpoint.
func Handler(s *mcp.Server, token string, log *slog.Logger) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless:    true, // survives instance restarts and sleeps; no session state needed
		JSONResponse: true,
		Logger:       log,
	})
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.Error(w, `{"error":"MCP is not configured (MCP_TOKEN)"}`, http.StatusServiceUnavailable)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gitpulse"`)
			http.Error(w, `{"error":"invalid or missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		h.ServeHTTP(w, r)
	})
}
