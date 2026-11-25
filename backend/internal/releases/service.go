package releases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

var (
	// ErrNotAllowed: the repo's "Draft releases" switch is off.
	ErrNotAllowed = errors.New("draft releases are turned off for this repository (Settings → Repository automation)")
	ErrInvalid    = errors.New("invalid release")
)

// GitHub is the read side; *github.HTTPClient implements it.
type GitHub interface {
	Repo(ctx context.Context, repo github.RepoRef) (*github.RepoInfo, error)
	LatestRelease(ctx context.Context, repo github.RepoRef) (*github.Release, error)
	Tags(ctx context.Context, repo github.RepoRef) ([]github.Tag, error)
	CommitDate(ctx context.Context, repo github.RepoRef, sha string) (time.Time, error)
	MergedPRs(ctx context.Context, repo github.RepoRef, base string, since time.Time, max int) ([]github.MergedPR, error)
}

// Writer creates draft releases with a token scoped to one repo.
type Writer interface {
	CreateDraftRelease(ctx context.Context, repo github.RepoRef, tag, target, name, body string) (*github.Release, error)
}

type Access interface {
	Read(ctx context.Context, u *store.User) (GitHub, error)
	ReleaseWriter(ctx context.Context, u *store.User, repoID int64) (Writer, error)
}

type Service struct {
	db     *pgxpool.Pool
	store  *store.Store
	access Access
	ai     *llm.Service
	log    *slog.Logger
}

func New(db *pgxpool.Pool, st *store.Store, access Access, ai *llm.Service, log *slog.Logger) *Service {
	return &Service{db: db, store: st, access: access, ai: ai, log: log}
}

const maxPRs = 200

type Preview struct {
	RepoID           int64      `json:"repoId"`
	Repo             string     `json:"repo"`
	DefaultBranch    string     `json:"defaultBranch"`
	BaseTag          string     `json:"baseTag"`    // "" = no previous release or tag
	BaseSource       string     `json:"baseSource"` // release | tag | ""
	BaseDate         *time.Time `json:"baseDate,omitempty"`
	SuggestedVersion string     `json:"suggestedVersion"`
	Bump             string     `json:"bump"` // major | minor | patch | initial
	Groups           []Group    `json:"groups"`
	Count            int        `json:"count"`
	Truncated        bool       `json:"truncated"` // more than maxPRs merged
	Intro            string     `json:"intro,omitempty"`
	Markdown         string     `json:"markdown"`
	CompareURL       string     `json:"compareUrl,omitempty"`
	AIStatus         string     `json:"aiStatus"` // ok | cached | disabled | over_budget | too_large | refused | error | skipped
	ReleasesEnabled  bool       `json:"releasesEnabled"`
}

func ref(fullName string) github.RepoRef {
	o, n, _ := strings.Cut(fullName, "/")
	return github.RepoRef{Owner: o, Name: n}
}

// Preview builds notes for everything merged since the last release.
func (s *Service) Preview(ctx context.Context, u *store.User, repoID int64) (*Preview, error) {
	rs, err := s.store.RepoSettingFor(ctx, u.ID, repoID)
	if err != nil {
		return nil, err
	}
	if rs.Source == "watched" {
		return nil, fmt.Errorf("%w: watched repositories are read-only", ErrNotAllowed)
	}
	gh, err := s.access.Read(ctx, u)
	if err != nil {
		return nil, err
	}
	r := ref(rs.FullName)
	info, err := gh.Repo(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("repo: %w", err)
	}
	p := &Preview{RepoID: repoID, Repo: info.FullName, DefaultBranch: info.DefaultBranch, ReleasesEnabled: rs.ReleasesEnabled, Groups: []Group{}}
	r = ref(info.FullName) // follows renames

	since, err := s.base(ctx, gh, r, p)
	if err != nil {
		return nil, err
	}
	prs, err := gh.MergedPRs(ctx, r, info.DefaultBranch, since, maxPRs+1)
	if err != nil {
		return nil, fmt.Errorf("merged PRs: %w", err)
	}
	if len(prs) > maxPRs {
		prs, p.Truncated = prs[:maxPRs], true
	}
	p.Count = len(prs)
	if built := Build(prs); built != nil {
		p.Groups = built
	}
	p.SuggestedVersion, p.Bump = Suggest(p.BaseTag, p.Groups)
	if p.BaseTag != "" {
		p.CompareURL = fmt.Sprintf("https://github.com/%s/compare/%s...%s", info.FullName, p.BaseTag, p.SuggestedVersion)
	}

	p.AIStatus = "skipped"
	if len(prs) > 0 {
		p.AIStatus = s.summarize(ctx, u.ID, repoID, prs, p)
	}
	p.Markdown = Markdown(p.Groups, p.Intro, p.CompareURL)
	return p, nil
}

// base finds the previous release: the latest published release, else the
// highest semver tag. It returns the cut-off time (zero = from the start).
func (s *Service) base(ctx context.Context, gh GitHub, r github.RepoRef, p *Preview) (time.Time, error) {
	rel, err := gh.LatestRelease(ctx, r)
	if err != nil {
		return time.Time{}, fmt.Errorf("latest release: %w", err)
	}
	tags, err := gh.Tags(ctx, r)
	if err != nil {
		return time.Time{}, fmt.Errorf("tags: %w", err)
	}
	var tag github.Tag
	switch {
	case rel != nil:
		p.BaseTag, p.BaseSource = rel.TagName, "release"
		for _, t := range tags {
			if t.Name == rel.TagName {
				tag = t
			}
		}
		if tag.SHA == "" { // tag not in the first 300: fall back to the release date
			d := rel.CreatedAt
			p.BaseDate = &d
			return d, nil
		}
	default:
		t, ok := HighestTag(tags)
		if !ok {
			return time.Time{}, nil
		}
		tag = t
		p.BaseTag, p.BaseSource = t.Name, "tag"
	}
	d, err := gh.CommitDate(ctx, r, tag.SHA)
	if err != nil {
		return time.Time{}, fmt.Errorf("tag commit: %w", err)
	}
	p.BaseDate = &d
	return d, nil
}

// --- optional AI rewrite ---

const promptVersion = "release-notes-v1"

const systemPrompt = `You write release notes for a software project. You get the pull requests merged since the last release, already grouped. Write:
- intro: one or two plain sentences summarising the release for users. No headings, no version numbers.
- items: for each pull request number given, one short line (under 120 characters) describing the change from a user's point of view, starting with a verb in the past or present tense. Keep technical terms; do not invent details beyond the title and description.
` + llm.UntrustedRules

var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"intro": map[string]any{"type": "string"},
		"items": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"number":  map[string]any{"type": "integer"},
				"summary": map[string]any{"type": "string"},
			},
			"required":             []string{"number", "summary"},
			"additionalProperties": false,
		}},
	},
	"required":             []string{"intro", "items"},
	"additionalProperties": false,
}

func (s *Service) summarize(ctx context.Context, userID, repoID int64, prs []github.MergedPR, p *Preview) string {
	if !s.ai.Enabled() {
		return "disabled"
	}
	groupOf := map[int]string{}
	for _, g := range p.Groups {
		for _, it := range g.Items {
			groupOf[it.Number] = g.Title
		}
	}
	var b strings.Builder
	for _, pr := range prs {
		body := pr.Body
		if len(body) > 600 {
			body = body[:600] + " [...]"
		}
		fmt.Fprintf(&b, "#%d [%s] %s\n%s\n\n", pr.Number, groupOf[pr.Number], pr.Title, body)
	}
	var out struct {
		Intro string `json:"intro"`
		Items []struct {
			Number  int    `json:"number"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	meta, err := s.ai.Run(ctx, llm.Task{
		Feature: "release_notes", UserID: userID, RepoID: repoID, PromptVersion: promptVersion,
		System: systemPrompt, Input: llm.WrapUntrusted("merged pull requests", b.String()), Schema: schema,
	}, &out)
	switch {
	case errors.Is(err, llm.ErrDisabled):
		return "disabled"
	case errors.Is(err, llm.ErrOverBudget):
		return "over_budget"
	case errors.Is(err, llm.ErrTooLarge):
		return "too_large"
	case errors.Is(err, llm.ErrRefused):
		return "refused"
	case err != nil:
		s.log.Warn("release notes AI", "err", err)
		return "error"
	}
	summaries := map[int]string{}
	for _, it := range out.Items {
		summaries[it.Number] = llm.SanitizeLine(it.Summary, 160)
	}
	for gi := range p.Groups {
		for ii := range p.Groups[gi].Items {
			p.Groups[gi].Items[ii].Summary = summaries[p.Groups[gi].Items[ii].Number]
		}
	}
	p.Intro = llm.SanitizeText(out.Intro, 500)
	if meta.Cached {
		return "cached"
	}
	return "ok"
}

// --- create ---

type CreateInput struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Target  string `json:"target"`  // branch or SHA; default branch when empty
	BaseTag string `json:"baseTag"` // for the audit log
}

// validTag follows git's ref-name rules closely enough for release tags.
func validTag(t string) bool {
	if t == "" || len(t) > 100 || strings.HasPrefix(t, "-") || strings.HasPrefix(t, ".") ||
		strings.HasSuffix(t, ".") || strings.HasSuffix(t, ".lock") || strings.Contains(t, "..") ||
		strings.Contains(t, "@{") || strings.HasSuffix(t, "/") || strings.Contains(t, "//") {
		return false
	}
	for _, r := range t {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// Create makes a DRAFT release, only if the repo's switch allows it.
func (s *Service) Create(ctx context.Context, u *store.User, repoID int64, in CreateInput, source string) (*github.Release, error) {
	rs, err := s.store.RepoSettingFor(ctx, u.ID, repoID)
	if err != nil {
		return nil, err
	}
	if !rs.ReleasesEnabled {
		return nil, ErrNotAllowed
	}
	in.Tag = strings.TrimSpace(in.Tag)
	if !validTag(in.Tag) {
		return nil, fmt.Errorf("%w: %q is not a valid tag name", ErrInvalid, in.Tag)
	}
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
		in.Name = in.Tag
	}
	if len(in.Name) > 200 || len(in.Body) > 120_000 || len(in.Target) > 250 {
		return nil, fmt.Errorf("%w: name, notes or target too long", ErrInvalid)
	}
	w, err := s.access.ReleaseWriter(ctx, u, repoID)
	if err != nil {
		return nil, err
	}
	rel, err := w.CreateDraftRelease(ctx, ref(rs.FullName), in.Tag, strings.TrimSpace(in.Target), in.Name, in.Body)
	if err != nil {
		var ae *github.APIError
		if errors.As(err, &ae) && ae.Status == 422 {
			return nil, fmt.Errorf("%w: GitHub rejected it (does a release for %s already exist?)", ErrInvalid, in.Tag)
		}
		return nil, err
	}
	if !rel.Draft {
		// Never expected (draft is hardcoded); make it loud if GitHub ignores it.
		s.log.Error("release created but not a draft", "repo", rs.FullName, "tag", in.Tag, "id", rel.ID)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO release_drafts (user_id, repo_id, repo_name, tag, base_tag, github_release_id, html_url, source)
		VALUES ($1,$2,$3,$4,nullif($5,''),$6,$7,$8)`,
		u.ID, repoID, rs.FullName, in.Tag, in.BaseTag, rel.ID, rel.HTMLURL, source); err != nil {
		s.log.Error("record release draft", "err", err)
	}
	s.log.Info("draft release created", "repo", rs.FullName, "tag", in.Tag, "source", source, "url", rel.HTMLURL)
	return rel, nil
}

type DraftRow struct {
	ID        int64     `json:"id"`
	Repo      string    `json:"repo"`
	Tag       string    `json:"tag"`
	BaseTag   string    `json:"baseTag,omitempty"`
	URL       string    `json:"url"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

// Drafts lists drafts gitpulse created for the user, newest first.
func (s *Service) Drafts(ctx context.Context, userID int64) ([]DraftRow, error) {
	rows, err := s.db.Query(ctx, `SELECT id, repo_name, tag, coalesce(base_tag,''), html_url, source, created_at
		FROM release_drafts WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftRow{}
	for rows.Next() {
		var d DraftRow
		if err := rows.Scan(&d.ID, &d.Repo, &d.Tag, &d.BaseTag, &d.URL, &d.Source, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
