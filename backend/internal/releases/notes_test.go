package releases

import (
	"strings"
	"testing"
	"time"

	"github.com/harshit3011/gitpulse/backend/internal/github"
)

func mpr(n int, title, author string, labels ...string) github.MergedPR {
	return github.MergedPR{Number: n, Title: title, Author: author, Labels: labels,
		URL: "https://github.com/o/r/pull/" + string(rune('0'+n)), MergedAt: time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC)}
}

func TestBuildGroupsByLabelThenPrefix(t *testing.T) {
	groups := Build([]github.MergedPR{
		mpr(1, "Add dark mode", "me", "enhancement"),               // label
		mpr(2, "fix: crash on empty repo", "me"),                   // prefix
		mpr(3, "feat: export CSV", "me", "bug"),                    // label beats prefix
		mpr(4, "Bump left-pad from 1.0 to 1.1", "dependabot[bot]"), // bot
		mpr(5, "chore(deps): update go deps", "me"),                // deps scope
		mpr(6, "ci: cache npm", "me"),                              // internal prefix
		mpr(7, "Update things", "me"),                              // nothing → other
		mpr(8, "docs: setup guide", "me", "documentation", "bug"),  // bug outranks docs
		mpr(9, "perf: faster scan", "me"),                          // perf → features
	})
	got := map[string][]int{}
	var order []string
	for _, g := range groups {
		order = append(order, g.Key)
		for _, it := range g.Items {
			got[g.Key] = append(got[g.Key], it.Number)
		}
	}
	want := map[string][]int{
		GroupFeatures: {1, 9}, GroupFixes: {2, 3, 8}, GroupDependencies: {4, 5},
		GroupInternal: {6}, GroupOther: {7},
	}
	for k, w := range want {
		if fmtInts(got[k]) != fmtInts(w) {
			t.Errorf("%s = %v want %v", k, got[k], w)
		}
	}
	if strings.Join(order, ",") != "features,fixes,other,internal,dependencies" {
		t.Errorf("order = %v", order)
	}
	if groups[1].Items[1].Why != "label bug" {
		t.Errorf("why = %q", groups[1].Items[1].Why)
	}
}

func fmtInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = string(rune('0' + n))
	}
	return strings.Join(s, ",")
}

func TestSuggestVersion(t *testing.T) {
	fix := []Group{{Key: GroupFixes, Items: []Item{{Number: 1}}}}
	feat := []Group{{Key: GroupFeatures, Items: []Item{{Number: 1}}}}
	brk := []Group{{Key: GroupFixes, Items: []Item{{Number: 1, Breaking: true}}}}
	deps := []Group{{Key: GroupDependencies, Items: []Item{{Number: 1}}}}
	cases := []struct {
		base   string
		groups []Group
		want   string
		bump   string
	}{
		{"v1.2.3", fix, "v1.2.4", "patch"},
		{"v1.2.3", feat, "v1.3.0", "minor"},
		{"v1.2.3", brk, "v2.0.0", "major"},
		{"v0.4.1", brk, "v0.5.0", "minor"}, // 0.x: breaking bumps minor
		{"1.2.3", deps, "1.2.4", "patch"},  // keeps the no-"v" style
		{"v2.0.0-rc.1", fix, "v2.0.1", "patch"},
		{"", feat, "v0.1.0", "initial"},
		{"release-2024", feat, "v0.1.0", "initial"},
	}
	for _, c := range cases {
		v, b := Suggest(c.base, c.groups)
		if v != c.want || b != c.bump {
			t.Errorf("%s: got %s/%s want %s/%s", c.base, v, b, c.want, c.bump)
		}
	}
}

func TestHighestTagIgnoresNonSemver(t *testing.T) {
	tag, ok := HighestTag([]github.Tag{{Name: "v1.10.0"}, {Name: "v1.9.9"}, {Name: "latest"}, {Name: "v1.10.0-rc.1"}, {Name: "v2.0.0-beta"}})
	if !ok || tag.Name != "v2.0.0-beta" {
		t.Fatalf("got %v", tag)
	}
	if _, ok := HighestTag([]github.Tag{{Name: "nightly"}}); ok {
		t.Fatal("non-semver tag accepted")
	}
}

func TestBreakingDetection(t *testing.T) {
	for _, pr := range []github.MergedPR{
		{Title: "feat!: drop v1 API"},
		{Title: "change config", Body: "BREAKING CHANGE: renamed X"},
		{Title: "rework", Labels: []string{"Breaking Change"}},
	} {
		if !Classify(pr).Breaking {
			t.Errorf("%+v not breaking", pr)
		}
	}
}

func TestMarkdown(t *testing.T) {
	groups := Build([]github.MergedPR{mpr(1, "feat!: new API", "alice"), mpr(2, "fix: typo", "bob")})
	md := Markdown(groups, "", "https://github.com/o/r/compare/v1.0.0...v2.0.0")
	for _, want := range []string{"## Features\n\n- **Breaking:** feat!: new API by @alice in https://github.com/o/r/pull/1",
		"## Fixes\n\n- fix: typo by @bob", "**Full changelog:** https://github.com/o/r/compare/v1.0.0...v2.0.0"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	groups[0].Items[0].Summary = "Added a new API"
	if md := Markdown(groups, "A big release.", ""); !strings.HasPrefix(md, "A big release.\n\n## Features") || !strings.Contains(md, "Added a new API by @alice") {
		t.Errorf("AI summary not used:\n%s", md)
	}
}

func TestValidTag(t *testing.T) {
	for _, ok := range []string{"v1.2.3", "1.0.0", "release/2026-11", "v2.0.0-rc.1"} {
		if !validTag(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "v1 2", "a..b", "-x", "x.lock", "x~1", "x^", "a:b", "x?", "a*", "x[", "x\\y", "x/", "@{x"} {
		if validTag(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
