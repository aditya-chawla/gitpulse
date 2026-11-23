// Package prreview is the PR assistant: it reviews each new head commit of a
// pull request once (keyed by repo + PR + head SHA), stores the result for
// the dashboard, and optionally keeps one marked comment up to date on the PR.
//
// Everything in this file is deterministic and works without AI.
package prreview

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/detect"
	"github.com/harshit3011/gitpulse/backend/internal/github"
)

// Flag is one notable or risky change found by rules.
type Flag struct {
	Kind   string   `json:"kind"`  // migration | auth | config | dependencies | deleted_tests | large_file | binary
	Title  string   `json:"title"` // human label
	Files  []string `json:"files"` // up to 10 paths
	Extra  int      `json:"extra"` // further files not listed
	Detail string   `json:"detail,omitempty"`
}

// Omitted is a file left out of the diff sent to AI (and of review detail).
type Omitted struct {
	Path   string `json:"path"`
	Reason string `json:"reason"` // lock file | generated | vendored | binary or too large | truncated | over size limit
}

type Analysis struct {
	FilesChanged    int       `json:"filesChanged"`
	FilesListed     int       `json:"filesListed"` // GitHub lists at most 3000
	Additions       int       `json:"additions"`
	Deletions       int       `json:"deletions"`
	Flags           []Flag    `json:"flags"`
	Omitted         []Omitted `json:"omitted"`
	SuggestedLabels []string  `json:"suggestedLabels"` // existing repo labels only
	// LabelReasons explains each suggestion (label -> why).
	LabelReasons map[string]string `json:"labelReasons"`
}

var (
	lockFiles = map[string]bool{
		"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true, "bun.lock": true,
		"go.sum": true, "Cargo.lock": true, "poetry.lock": true, "Pipfile.lock": true, "Gemfile.lock": true,
		"composer.lock": true, "uv.lock": true, "pubspec.lock": true, "mix.lock": true, "flake.lock": true,
		"npm-shrinkwrap.json": true, "gradle.lockfile": true, "packages.lock.json": true,
	}
	vendoredDirs  = map[string]bool{"vendor": true, "node_modules": true, "third_party": true, "third-party": true, "bower_components": true, ".yarn": true}
	generatedDirs = map[string]bool{"dist": true, "build": true, "generated": true, "__generated__": true, "__snapshots__": true, ".next": true, "out": true, "coverage": true}
	generatedName = regexp.MustCompile(`(\.min\.(js|css)|\.map|\.pb\.go|_pb2\.py|\.pb\.(cc|h)|[._]generated\.\w+|\.gen\.\w+|_gen\.go|\.snap)$`)

	migrationPath = regexp.MustCompile(`(?i)(^|/)(migrations?|migrate|alembic|flyway|liquibase|prisma/migrations|db/migrate|schema)(/|$)|\.sql$`)
	authPath      = regexp.MustCompile(`(?i)(^|[/._-])(auth\w*|login|logout|session\w*|oauth\w*|jwt|passwords?|crypto|permissions?|rbac|acl|tokens?|secrets?|credentials?|csrf|cors|middleware)([/._-]|$)`)
	configPath    = regexp.MustCompile(`(?i)(^|/)(\.github/workflows/|dockerfile|docker-compose[^/]*$|compose\.ya?ml$|\.env[^/]*$|render\.yaml$|vercel\.json$|netlify\.toml$|fly\.toml$|procfile$|nginx[^/]*\.conf$|k8s/|kubernetes/|helm/|terraform/|[^/]+\.tf$|\.gitlab-ci\.yml$|config/|settings\.py$|application\.(ya?ml|properties)$)`)
)

// classifyOmit returns why a file is left out of the AI diff, or "".
func classifyOmit(f github.PRFile) string {
	segs := strings.Split(f.Path, "/")
	base := segs[len(segs)-1]
	if lockFiles[base] {
		return "lock file"
	}
	for _, s := range segs[:len(segs)-1] {
		if vendoredDirs[s] {
			return "vendored"
		}
		if generatedDirs[s] {
			return "generated"
		}
	}
	if generatedName.MatchString(base) {
		return "generated"
	}
	if f.Patch == "" && f.Status != "removed" && f.Status != "renamed" {
		return "binary or too large"
	}
	return ""
}

// Limits for building the diff given to AI.
type DiffLimits struct {
	PerFileChars int // a file's patch beyond this is cut
	TotalChars   int // the whole diff; files that don't fit are listed, not sent
}

// BuildDiff applies the filters and size limits. It returns the diff text for
// AI and every file that was omitted or truncated, with the reason.
func BuildDiff(files []github.PRFile, lim DiffLimits) (string, []Omitted) {
	var (
		b       strings.Builder
		omitted []Omitted
	)
	// Smaller, source-looking files first so one huge file can't crowd out the rest.
	order := make([]int, len(files))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(files[a].Patch) - len(files[b].Patch) })
	included := make([]bool, len(files))
	parts := make([]string, len(files))
	for _, i := range order {
		f := files[i]
		if why := classifyOmit(f); why != "" {
			omitted = append(omitted, Omitted{Path: f.Path, Reason: why})
			continue
		}
		patch := f.Patch
		truncated := false
		if len(patch) > lim.PerFileChars {
			patch = cutAtLine(patch, lim.PerFileChars)
			truncated = true
		}
		header := fmt.Sprintf("--- %s (%s, +%d -%d)", f.Path, f.Status, f.Additions, f.Deletions)
		if f.PreviousPath != "" {
			header += " renamed from " + f.PreviousPath
		}
		part := header + "\n" + patch
		if truncated {
			part += fmt.Sprintf("\n[... truncated: %d of %d characters shown]", len(patch), len(f.Patch))
		}
		if b.Len()+len(part) > lim.TotalChars {
			omitted = append(omitted, Omitted{Path: f.Path, Reason: "over size limit"})
			continue
		}
		b.WriteString(part + "\n")
		if truncated {
			omitted = append(omitted, Omitted{Path: f.Path, Reason: "truncated"})
		}
		included[i] = true
		parts[i] = part
	}
	// Emit in the PR's original file order for readability.
	var out strings.Builder
	for i := range files {
		if included[i] {
			out.WriteString(parts[i] + "\n")
		}
	}
	slices.SortStableFunc(omitted, func(a, b Omitted) int { return strings.Compare(a.Path, b.Path) })
	return out.String(), omitted
}

func cutAtLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '\n'); i > n/2 {
		s = s[:i]
	}
	return s
}

const (
	largeFileLines = 500
	maxFlagFiles   = 10
)

// Flags finds notable/risky changes by path and size rules.
func Flags(files []github.PRFile) []Flag {
	type acc struct {
		title string
		files []string
	}
	order := []string{"migration", "auth", "config", "dependencies", "deleted_tests", "large_file", "binary"}
	found := map[string]*acc{
		"migration":     {title: "Database migrations or schema"},
		"auth":          {title: "Auth, sessions or security-sensitive code"},
		"config":        {title: "Deployment, CI or configuration"},
		"dependencies":  {title: "Dependency changes"},
		"deleted_tests": {title: "Deleted tests"},
		"large_file":    {title: fmt.Sprintf("Large changes (over %d lines in a file)", largeFileLines)},
		"binary":        {title: "Binary or unreviewable files"},
	}
	add := func(kind, p string) { found[kind].files = append(found[kind].files, p) }
	for _, f := range files {
		base := path.Base(f.Path)
		if migrationPath.MatchString(f.Path) {
			add("migration", f.Path)
		}
		if authPath.MatchString(f.Path) && !detect.IsTestFile(f.Path) {
			add("auth", f.Path)
		}
		if configPath.MatchString(f.Path) {
			add("config", f.Path)
		}
		if detect.IsManifest(base) || lockFiles[base] {
			add("dependencies", f.Path)
		}
		if f.Status == "removed" && detect.IsTestFile(f.Path) {
			add("deleted_tests", f.Path)
		}
		if f.Changes > largeFileLines && !lockFiles[base] {
			add("large_file", f.Path)
		}
		if f.Patch == "" && f.Status == "added" && f.Changes == 0 {
			add("binary", f.Path)
		}
	}
	var out []Flag
	for _, k := range order {
		a := found[k]
		if len(a.files) == 0 {
			continue
		}
		fl := Flag{Kind: k, Title: a.title, Files: a.files}
		if len(fl.Files) > maxFlagFiles {
			fl.Extra = len(fl.Files) - maxFlagFiles
			fl.Files = fl.Files[:maxFlagFiles]
		}
		out = append(out, fl)
	}
	return out
}

// --- rule-based label suggestions ---

// concept -> label names that mean it (normalised: lower case, no spaces,
// dashes, underscores or "type:" prefixes).
var labelSynonyms = map[string][]string{
	"feature":       {"enhancement", "feature", "feat", "newfeature", "featurerequest"},
	"bug":           {"bug", "fix", "bugfix", "defect"},
	"docs":          {"documentation", "docs", "doc"},
	"dependencies":  {"dependencies", "deps", "dependency", "dependencyupdate"},
	"ci":            {"ci", "githubactions", "cicd", "build"},
	"tests":         {"test", "tests", "testing"},
	"refactor":      {"refactor", "refactoring", "codequality", "cleanup"},
	"performance":   {"performance", "perf"},
	"chore":         {"chore", "maintenance", "housekeeping"},
	"breaking":      {"breaking", "breakingchange", "major"},
	"security":      {"security"},
	"database":      {"database", "db", "migration", "migrations"},
	"configuration": {"config", "configuration", "infra", "infrastructure", "devops"},
}

var conventional = regexp.MustCompile(`^(\w+)(\(([^)]*)\))?(!)?:\s`)

var prefixConcept = map[string]string{
	"feat": "feature", "feature": "feature", "fix": "bug", "bugfix": "bug", "hotfix": "bug",
	"docs": "docs", "doc": "docs", "test": "tests", "tests": "tests", "refactor": "refactor",
	"perf": "performance", "chore": "chore", "ci": "ci", "build": "ci", "deps": "dependencies",
	"security": "security", "style": "chore", "revert": "bug",
}

func normLabel(s string) string {
	s = strings.ToLower(s)
	if i := strings.LastIndexByte(s, ':'); i >= 0 { // "type: bug", "kind:feature"
		s = s[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' || r == '/' || r == '.' {
			return -1
		}
		return r
	}, s)
}

// ConventionalType parses "feat(scope)!: ..." into ("feat", "scope", breaking).
func ConventionalType(msg string) (typ, scope string, breaking bool) {
	first, rest, _ := strings.Cut(msg, "\n")
	m := conventional.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil {
		return "", "", strings.Contains(rest, "BREAKING CHANGE")
	}
	return strings.ToLower(m[1]), strings.ToLower(m[3]), m[4] == "!" || strings.Contains(rest, "BREAKING CHANGE")
}

// SuggestLabels proposes labels from the PR title, commit messages, author
// and changed paths, keeping only labels that exist in the repo. Labels
// already on the PR are not suggested again.
func SuggestLabels(pr github.PullRequest, commits []string, files []github.PRFile, repoLabels []string) ([]string, map[string]string) {
	reasons := map[string]string{} // concept -> reason
	note := func(concept, why string) {
		if _, ok := reasons[concept]; !ok {
			reasons[concept] = why
		}
	}
	if t, scope, br := ConventionalType(pr.Title); t != "" {
		if c, ok := prefixConcept[t]; ok {
			note(c, fmt.Sprintf("title starts with %q", t+":"))
		}
		if scope == "deps" || scope == "deps-dev" {
			note("dependencies", "title scope is deps")
		}
		if br {
			note("breaking", "title marks a breaking change")
		}
	}
	commitTypes := map[string]int{}
	for _, m := range commits {
		t, scope, br := ConventionalType(m)
		if c, ok := prefixConcept[t]; ok {
			commitTypes[c]++
		}
		if scope == "deps" {
			commitTypes["dependencies"]++
		}
		if br {
			note("breaking", "a commit marks a breaking change")
		}
	}
	// Commit prefixes count only when they agree (majority of commits).
	for c, n := range commitTypes {
		if len(commits) > 0 && n*2 > len(commits) {
			note(c, fmt.Sprintf("%d of %d commits are %s", n, len(commits), c))
		}
	}
	if login := strings.ToLower(pr.Author); strings.HasPrefix(login, "dependabot") || strings.HasPrefix(login, "renovate") {
		note("dependencies", "opened by "+pr.Author)
	}

	allMatch := func(pred func(string) bool) bool {
		if len(files) == 0 {
			return false
		}
		for _, f := range files {
			if !pred(f.Path) {
				return false
			}
		}
		return true
	}
	isDoc := func(p string) bool {
		l := strings.ToLower(p)
		return strings.HasSuffix(l, ".md") || strings.HasSuffix(l, ".mdx") || strings.HasSuffix(l, ".rst") ||
			strings.HasPrefix(l, "docs/") || strings.Contains(l, "/docs/") || path.Base(l) == "license"
	}
	switch {
	case allMatch(isDoc):
		note("docs", "only documentation files changed")
	case allMatch(detect.IsTestFile):
		note("tests", "only test files changed")
	case allMatch(func(p string) bool { return strings.HasPrefix(p, ".github/workflows/") }):
		note("ci", "only workflow files changed")
	case allMatch(func(p string) bool { b := path.Base(p); return detect.IsManifest(b) || lockFiles[b] }):
		note("dependencies", "only dependency manifests changed")
	}
	for _, f := range files {
		if migrationPath.MatchString(f.Path) {
			note("database", "touches "+f.Path)
			break
		}
	}

	existing := map[string]string{} // normalised -> real name
	for _, l := range repoLabels {
		existing[normLabel(l)] = l
	}
	onPR := map[string]bool{}
	for _, l := range pr.Labels {
		onPR[strings.ToLower(l)] = true
	}
	var labels []string
	why := map[string]string{}
	concepts := make([]string, 0, len(reasons))
	for c := range reasons {
		concepts = append(concepts, c)
	}
	slices.Sort(concepts)
	for _, c := range concepts {
		for _, syn := range labelSynonyms[c] {
			if real, ok := existing[syn]; ok {
				if !onPR[strings.ToLower(real)] && why[real] == "" {
					labels = append(labels, real)
					why[real] = reasons[c]
				}
				break
			}
		}
	}
	return labels, why
}

// FilterLabels keeps only names that exist in the repo (case-insensitive),
// returning the repo's spelling. Used on AI suggestions.
func FilterLabels(suggested, repoLabels []string) []string {
	real := map[string]string{}
	for _, l := range repoLabels {
		real[strings.ToLower(strings.TrimSpace(l))] = l
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range suggested {
		if r, ok := real[strings.ToLower(strings.TrimSpace(s))]; ok && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// Analyze runs every deterministic check.
func Analyze(pr github.PullRequest, commits []string, files []github.PRFile, repoLabels []string, lim DiffLimits) (Analysis, string) {
	a := Analysis{FilesChanged: pr.ChangedFiles, FilesListed: len(files), Additions: pr.Additions, Deletions: pr.Deletions}
	if a.FilesChanged == 0 {
		a.FilesChanged = len(files)
	}
	a.Flags = Flags(files)
	diff, omitted := BuildDiff(files, lim)
	a.Omitted = omitted
	if a.FilesChanged > len(files) {
		a.Omitted = append(a.Omitted, Omitted{Path: fmt.Sprintf("%d more files", a.FilesChanged-len(files)), Reason: "not listed by GitHub (over 3000 files)"})
	}
	a.SuggestedLabels, a.LabelReasons = SuggestLabels(pr, commits, files, repoLabels)
	if a.SuggestedLabels == nil {
		a.SuggestedLabels = []string{}
	}
	if a.Flags == nil {
		a.Flags = []Flag{}
	}
	if a.Omitted == nil {
		a.Omitted = []Omitted{}
	}
	return a, diff
}
