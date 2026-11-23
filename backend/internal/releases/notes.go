// Package releases drafts release notes: it finds the last release (or
// highest semver tag), collects PRs merged since, groups them by labels and
// conventional-commit prefixes, suggests the next semver version, and can
// create a DRAFT GitHub release. It never publishes a release.
package releases

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/prreview"
)

// Group keys, in display order.
const (
	GroupFeatures     = "features"
	GroupFixes        = "fixes"
	GroupInternal     = "internal"
	GroupDependencies = "dependencies"
	GroupOther        = "other"
)

var groupTitles = map[string]string{
	GroupFeatures:     "Features",
	GroupFixes:        "Fixes",
	GroupInternal:     "Internal",
	GroupDependencies: "Dependencies",
	GroupOther:        "Other changes",
}

var groupOrder = []string{GroupFeatures, GroupFixes, GroupOther, GroupInternal, GroupDependencies}

type Item struct {
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Author   string   `json:"author"`
	Labels   []string `json:"labels"`
	Breaking bool     `json:"breaking"`
	// Summary is the AI rewrite of the title (empty without AI).
	Summary string `json:"summary,omitempty"`
	// Why says what decided the group: a label or a title prefix.
	Why string `json:"why"`
}

type Group struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// label (normalised) -> group. Checked before title prefixes.
var labelGroup = map[string]string{
	"enhancement": GroupFeatures, "feature": GroupFeatures, "feat": GroupFeatures, "newfeature": GroupFeatures,
	"performance": GroupFeatures, "perf": GroupFeatures,
	"bug": GroupFixes, "fix": GroupFixes, "bugfix": GroupFixes, "regression": GroupFixes, "security": GroupFixes,
	"dependencies": GroupDependencies, "deps": GroupDependencies, "dependency": GroupDependencies,
	"chore": GroupInternal, "ci": GroupInternal, "refactor": GroupInternal, "documentation": GroupInternal,
	"docs": GroupInternal, "test": GroupInternal, "tests": GroupInternal, "build": GroupInternal,
	"maintenance": GroupInternal, "internal": GroupInternal, "githubactions": GroupInternal,
}

var prefixGroup = map[string]string{
	"feat": GroupFeatures, "feature": GroupFeatures, "perf": GroupFeatures,
	"fix": GroupFixes, "bugfix": GroupFixes, "hotfix": GroupFixes, "revert": GroupFixes, "security": GroupFixes,
	"chore": GroupInternal, "ci": GroupInternal, "build": GroupInternal, "refactor": GroupInternal,
	"docs": GroupInternal, "doc": GroupInternal, "test": GroupInternal, "tests": GroupInternal, "style": GroupInternal,
	"deps": GroupDependencies,
}

var breakingLabels = map[string]bool{"breaking": true, "breakingchange": true, "major": true}

func norm(s string) string {
	s = strings.ToLower(s)
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' || r == '/' || r == '.' {
			return -1
		}
		return r
	}, s)
}

func isDepBot(login string) bool {
	l := strings.ToLower(login)
	return strings.HasPrefix(l, "dependabot") || strings.HasPrefix(l, "renovate")
}

// Classify turns a merged PR into an item and detects breaking changes
// ("!" after the prefix, "BREAKING CHANGE" in the body, or a breaking label).
func Classify(pr github.MergedPR) Item {
	it := Item{Number: pr.Number, Title: pr.Title, URL: pr.URL, Author: pr.Author, Labels: pr.Labels}
	_, _, breaking := prreview.ConventionalType(pr.Title)
	it.Breaking = breaking || strings.Contains(pr.Body, "BREAKING CHANGE")
	for _, l := range pr.Labels {
		if breakingLabels[norm(l)] {
			it.Breaking = true
		}
	}
	return it
}

// group decides an item's group and says why: dependency bots and deps
// scopes first, then labels, then the title's conventional-commit prefix;
// anything else is "other".
func group(pr github.MergedPR) (string, string) {
	if isDepBot(pr.Author) {
		return GroupDependencies, "opened by " + pr.Author
	}
	typ, scope, _ := prreview.ConventionalType(pr.Title)
	if scope == "deps" || scope == "deps-dev" {
		return GroupDependencies, "title scope " + scope
	}
	// Labels win over the prefix; the first label that maps decides, in a
	// fixed priority so "bug"+"documentation" counts as a fix.
	best, bestLabel := "", ""
	for _, l := range pr.Labels {
		g, ok := labelGroup[norm(l)]
		if ok && (best == "" || slices.Index(groupOrder, g) < slices.Index(groupOrder, best)) {
			best, bestLabel = g, l
		}
	}
	if best != "" {
		return best, "label " + bestLabel
	}
	if g, ok := prefixGroup[typ]; ok {
		return g, "title prefix " + typ + ":"
	}
	return GroupOther, "no label or prefix"
}

// Build groups merged PRs, oldest first within each group.
func Build(prs []github.MergedPR) []Group {
	byKey := map[string]*Group{}
	sorted := slices.Clone(prs)
	slices.SortStableFunc(sorted, func(a, b github.MergedPR) int { return a.MergedAt.Compare(b.MergedAt) })
	for _, pr := range sorted {
		it := Classify(pr)
		key, why := group(pr)
		it.Why = why
		g := byKey[key]
		if g == nil {
			g = &Group{Key: key, Title: groupTitles[key]}
			byKey[key] = g
		}
		g.Items = append(g.Items, it)
	}
	var out []Group
	for _, k := range groupOrder {
		if g := byKey[k]; g != nil {
			out = append(out, *g)
		}
	}
	return out
}

// --- semver ---

var semverRe = regexp.MustCompile(`^(v?)(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type Version struct {
	Prefix              string // "v" or ""
	Major, Minor, Patch int
	Pre                 string
}

func ParseVersion(tag string) (Version, bool) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(tag))
	if m == nil {
		return Version{}, false
	}
	a, _ := strconv.Atoi(m[2])
	b, _ := strconv.Atoi(m[3])
	c, _ := strconv.Atoi(m[4])
	return Version{Prefix: m[1], Major: a, Minor: b, Patch: c, Pre: m[5]}, true
}

func (v Version) String() string {
	s := fmt.Sprintf("%s%d.%d.%d", v.Prefix, v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Less orders versions; a pre-release sorts before its release.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	if v.Patch != o.Patch {
		return v.Patch < o.Patch
	}
	return v.Pre != "" && (o.Pre == "" || v.Pre < o.Pre)
}

// HighestTag returns the highest semver tag, ignoring non-semver names.
func HighestTag(tags []github.Tag) (github.Tag, bool) {
	var best github.Tag
	var bv Version
	found := false
	for _, t := range tags {
		v, ok := ParseVersion(t.Name)
		if !ok {
			continue
		}
		if !found || bv.Less(v) {
			best, bv, found = t, v, true
		}
	}
	return best, found
}

// Suggest proposes the next version. Breaking changes bump major (minor
// while below 1.0), features bump minor, anything else bumps patch. With no
// base tag the suggestion is v0.1.0.
func Suggest(baseTag string, groups []Group) (version, bump string) {
	breaking, features := false, false
	for _, g := range groups {
		for _, it := range g.Items {
			breaking = breaking || it.Breaking
		}
		if g.Key == GroupFeatures {
			features = true
		}
	}
	base, ok := ParseVersion(baseTag)
	if !ok {
		return "v0.1.0", "initial"
	}
	base.Pre = ""
	switch {
	case breaking && base.Major > 0:
		base.Major, base.Minor, base.Patch = base.Major+1, 0, 0
		bump = "major"
	case breaking || features:
		base.Minor, base.Patch = base.Minor+1, 0
		bump = "minor"
	default:
		base.Patch++
		bump = "patch"
	}
	return base.String(), bump
}

// Markdown renders the notes. Titles are written as-is (they're the repo's own
// PR titles, shown in the user's own draft); AI summaries are sanitised
// before they get here.
func Markdown(groups []Group, intro string, compareURL string) string {
	var b strings.Builder
	if intro != "" {
		b.WriteString(intro + "\n\n")
	}
	for _, g := range groups {
		fmt.Fprintf(&b, "## %s\n\n", g.Title)
		for _, it := range g.Items {
			text := it.Title
			if it.Summary != "" {
				text = it.Summary
			}
			if it.Breaking {
				text = "**Breaking:** " + text
			}
			fmt.Fprintf(&b, "- %s by @%s in %s\n", text, it.Author, it.URL)
		}
		b.WriteString("\n")
	}
	if compareURL != "" {
		b.WriteString("**Full changelog:** " + compareURL + "\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}
