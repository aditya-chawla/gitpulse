package prreview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/harshit3011/gitpulse/backend/internal/github"
)

func sign(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := `{"action":"opened"}`
	good := sign("s3cret", body)
	cases := []struct {
		name, secret, body, header string
		want                       bool
	}{
		{"valid", "s3cret", body, good, true},
		{"tampered body", "s3cret", body + " ", good, false},
		{"wrong secret", "other", body, good, false},
		{"sha1 header", "s3cret", body, "sha1=" + strings.TrimPrefix(good, "sha256="), false},
		{"missing", "s3cret", body, "", false},
		{"not hex", "s3cret", body, "sha256=zz", false},
		{"empty secret never verifies", "", body, sign("", body), false},
	}
	for _, c := range cases {
		if got := VerifySignature([]byte(c.secret), []byte(c.body), c.header); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestBuildDiffFiltersAndTruncates(t *testing.T) {
	big := strings.Repeat("+line of code\n", 1000) // ~14k chars
	files := []github.PRFile{
		{Path: "src/app.go", Status: "modified", Additions: 3, Patch: "@@ -1 +1 @@\n-a\n+b"},
		{Path: "package-lock.json", Status: "modified", Patch: "@@ lock @@"},
		{Path: "vendor/x/y.go", Status: "added", Patch: "+vendored"},
		{Path: "web/dist/app.js", Status: "modified", Patch: "+built"},
		{Path: "static/app.min.js", Status: "modified", Patch: "+min"},
		{Path: "logo.png", Status: "added"},
		{Path: "src/big.go", Status: "modified", Additions: 1000, Patch: big},
		{Path: "src/huge2.go", Status: "modified", Patch: strings.Repeat("x", 5000)},
	}
	diff, omitted := BuildDiff(files, DiffLimits{PerFileChars: 6000, TotalChars: 7000})
	reasons := map[string]string{}
	for _, o := range omitted {
		reasons[o.Path] = o.Reason
	}
	want := map[string]string{
		"package-lock.json": "lock file", "vendor/x/y.go": "vendored", "web/dist/app.js": "generated",
		"static/app.min.js": "generated", "logo.png": "binary or too large",
	}
	for p, r := range want {
		if reasons[p] != r {
			t.Errorf("%s: reason %q want %q", p, reasons[p], r)
		}
	}
	// Smallest files go first: app.go and huge2.go fit; big.go (truncated to
	// 6000) no longer fits in the 7000 total.
	if !strings.Contains(diff, "--- src/app.go") || !strings.Contains(diff, "--- src/huge2.go") {
		t.Errorf("expected small files in diff:\n%s", diff[:200])
	}
	if reasons["src/big.go"] != "over size limit" {
		t.Errorf("big.go: %q", reasons["src/big.go"])
	}
	for _, bad := range []string{"lock @@", "+vendored", "+built", "+min"} {
		if strings.Contains(diff, bad) {
			t.Errorf("filtered content %q leaked into diff", bad)
		}
	}

	// Per-file truncation is reported, and cuts at a line boundary.
	diff, omitted = BuildDiff([]github.PRFile{{Path: "src/big.go", Status: "modified", Patch: big}}, DiffLimits{PerFileChars: 1000, TotalChars: 50_000})
	if len(omitted) != 1 || omitted[0].Reason != "truncated" || !strings.Contains(diff, "[... truncated:") {
		t.Fatalf("truncation not reported: %v", omitted)
	}
	if strings.Contains(diff, "+line of co\n") {
		t.Error("cut mid-line")
	}
}

func TestFlags(t *testing.T) {
	files := []github.PRFile{
		{Path: "backend/internal/db/migrations/0004_x.up.sql", Status: "added", Changes: 10, Patch: "+"},
		{Path: "backend/internal/auth/auth.go", Status: "modified", Changes: 5, Patch: "+"},
		{Path: "backend/internal/auth/auth_test.go", Status: "removed", Changes: 50, Patch: "-"},
		{Path: ".github/workflows/ci.yml", Status: "modified", Changes: 2, Patch: "+"},
		{Path: "frontend/package.json", Status: "modified", Changes: 2, Patch: "+"},
		{Path: "src/generated_big.ts", Status: "modified", Changes: 900, Patch: "+"},
		{Path: "docs/diagram.png", Status: "added", Changes: 0},
		{Path: "README.md", Status: "modified", Changes: 3, Patch: "+"},
	}
	got := map[string][]string{}
	for _, f := range Flags(files) {
		got[f.Kind] = f.Files
	}
	want := map[string]string{
		"migration": "backend/internal/db/migrations/0004_x.up.sql", "auth": "backend/internal/auth/auth.go",
		"deleted_tests": "backend/internal/auth/auth_test.go", "config": ".github/workflows/ci.yml",
		"dependencies": "frontend/package.json", "large_file": "src/generated_big.ts", "binary": "docs/diagram.png",
	}
	for k, p := range want {
		if !slices.Contains(got[k], p) {
			t.Errorf("%s: got %v want %s", k, got[k], p)
		}
	}
	if slices.Contains(got["auth"], "backend/internal/auth/auth_test.go") {
		t.Error("test files are not auth risk")
	}
	if len(got) != len(want) {
		t.Errorf("unexpected flags: %v", got)
	}
}

func TestSuggestLabelsOnlyExisting(t *testing.T) {
	repo := []string{"bug", "enhancement", "Type: Documentation"}
	pr := github.PullRequest{Title: "feat(api): add sync endpoint"}
	labels, why := SuggestLabels(pr, nil, []github.PRFile{{Path: "src/a.go"}}, repo)
	if !slices.Equal(labels, []string{"enhancement"}) || why["enhancement"] == "" {
		t.Fatalf("feat -> %v %v", labels, why)
	}

	// Docs-only change, label spelled differently in the repo.
	labels, _ = SuggestLabels(github.PullRequest{Title: "Update readme"}, nil,
		[]github.PRFile{{Path: "README.md"}, {Path: "docs/setup.md"}}, repo)
	if !slices.Equal(labels, []string{"Type: Documentation"}) {
		t.Fatalf("docs -> %v", labels)
	}

	// Concepts with no matching repo label are dropped (no "dependencies" label here).
	labels, _ = SuggestLabels(github.PullRequest{Title: "chore(deps): bump x", Author: "dependabot[bot]"}, nil,
		[]github.PRFile{{Path: "go.mod"}, {Path: "go.sum"}}, repo)
	if len(labels) != 0 {
		t.Fatalf("deps with no label -> %v", labels)
	}

	// Commit prefixes count only by majority; existing PR labels aren't re-suggested.
	pr = github.PullRequest{Title: "Various", Labels: []string{"bug"}}
	labels, _ = SuggestLabels(pr, []string{"fix: a", "fix: b", "feat: c"}, []github.PRFile{{Path: "a.go"}}, repo)
	if len(labels) != 0 {
		t.Fatalf("majority fix but bug already on PR -> %v", labels)
	}
}

func TestFilterLabels(t *testing.T) {
	got := FilterLabels([]string{"BUG", "made-up", "enhancement", "bug"}, []string{"bug", "enhancement"})
	if !slices.Equal(got, []string{"bug", "enhancement"}) {
		t.Fatalf("got %v", got)
	}
}

func TestConventionalType(t *testing.T) {
	for in, want := range map[string][3]string{
		"feat(ui)!: redo nav":              {"feat", "ui", "true"},
		"fix: crash\n\nBREAKING CHANGE: x": {"fix", "", "true"},
		"Update README":                    {"", "", "false"},
		"chore(deps): bump":                {"chore", "deps", "false"},
	} {
		typ, scope, br := ConventionalType(in)
		if typ != want[0] || scope != want[1] || (br && want[2] != "true") || (!br && want[2] == "true") {
			t.Errorf("%q -> %q %q %v", in, typ, scope, br)
		}
	}
}

func TestRenderCommentNoAI(t *testing.T) {
	r := Result{HeadSHA: "abcdef1234567", Analysis: Analysis{
		FilesChanged: 2, Additions: 10, Deletions: 1,
		Flags:           []Flag{{Kind: "migration", Title: "Database migrations or schema", Files: []string{"db/1.sql"}}},
		Omitted:         []Omitted{{Path: "package-lock.json", Reason: "lock file"}},
		SuggestedLabels: []string{"bug"},
	}}
	body := RenderComment(r, "")
	for _, want := range []string{Marker, "`abcdef1`", "**2 files changed** · +10 −1", "#### Notable changes", "`db/1.sql`",
		"#### Suggested labels\n`bug`", "Left out of the review (1)", "never changes titles or labels"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Summary") || strings.Contains(body, "Suggested title") {
		t.Error("no-AI comment must not have a summary or title section")
	}
	if !strings.HasPrefix(body, Marker) || strings.Count(body, Marker) != 1 {
		t.Error("marker must appear exactly once, first")
	}
}
