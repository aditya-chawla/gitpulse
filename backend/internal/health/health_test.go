package health

import (
	"testing"
	"time"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func daysAgo(d int) *time.Time { t := now.Add(-time.Duration(d) * 24 * time.Hour); return &t }

func perfect() Input {
	return Input{
		HasReadme: true, HasLicense: true, HasCI: true, HasTests: true,
		HasDescription: true, TopicCount: 3, PushedAt: daysAgo(2),
		Branches: []Branch{{Name: "main", LastCommitAt: daysAgo(400), IsDefault: true}},
	}
}

func pointsOf(r Result, key string) int {
	for _, c := range r.Checks {
		if c.Key == key {
			return c.Points
		}
	}
	panic("no check " + key)
}

func TestPerfectRepoScores100(t *testing.T) {
	r := Evaluate(perfect(), now)
	if r.Score != 100 || r.Status != Active {
		t.Fatalf("got %d %s: %+v", r.Score, r.Status, r.Checks)
	}
	total := 0
	for _, c := range r.Checks {
		total += c.Max
	}
	if total != 100 {
		t.Fatalf("check maxima sum to %d, want 100", total)
	}
}

func TestBareRepoScoresOnlyAbsenceChecks(t *testing.T) {
	// Nothing present: only "no stale branches" and "no old PRs" are earned.
	r := Evaluate(Input{}, now)
	if r.Score != 20 || r.Status != Dormant {
		t.Fatalf("got %d %s", r.Score, r.Status)
	}
}

func TestChecks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Input)
		key    string
		want   int
	}{
		{"no readme", func(i *Input) { i.HasReadme = false }, "readme", 0},
		{"no license", func(i *Input) { i.HasLicense = false }, "license", 0},
		{"no ci", func(i *Input) { i.HasCI = false }, "ci", 0},
		{"no tests", func(i *Input) { i.HasTests = false }, "tests", 0},
		{"push 30d is full", func(i *Input) { i.PushedAt = daysAgo(30) }, "activity", 15},
		{"push 60d is half", func(i *Input) { i.PushedAt = daysAgo(60) }, "activity", 8},
		{"push 91d is zero", func(i *Input) { i.PushedAt = daysAgo(91) }, "activity", 0},
		{"default branch never stale", func(i *Input) {}, "stale_branches", 10},
		{"branch at 90d not stale", func(i *Input) {
			i.Branches = append(i.Branches, Branch{Name: "f", LastCommitAt: daysAgo(90)})
		}, "stale_branches", 10},
		{"one stale branch", func(i *Input) {
			i.Branches = append(i.Branches, Branch{Name: "f", LastCommitAt: daysAgo(91)})
		}, "stale_branches", 7},
		{"many stale branches floor at 0", func(i *Input) {
			for range 5 {
				i.Branches = append(i.Branches, Branch{Name: "f", LastCommitAt: daysAgo(200)})
			}
		}, "stale_branches", 0},
		{"fresh PR ok", func(i *Input) { i.OpenPRCreated = []time.Time{*daysAgo(10)} }, "old_prs", 10},
		{"one old PR", func(i *Input) { i.OpenPRCreated = []time.Time{*daysAgo(31)} }, "old_prs", 5},
		{"three old PRs floor at 0", func(i *Input) {
			i.OpenPRCreated = []time.Time{*daysAgo(31), *daysAgo(40), *daysAgo(99)}
		}, "old_prs", 0},
		{"no topics", func(i *Input) { i.TopicCount = 0 }, "metadata", 5},
		{"no description", func(i *Input) { i.HasDescription = false }, "metadata", 5},
		{"neither", func(i *Input) { i.HasDescription = false; i.TopicCount = 0 }, "metadata", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := perfect()
			tc.mutate(&in)
			r := Evaluate(in, now)
			if got := pointsOf(r, tc.key); got != tc.want {
				t.Fatalf("%s = %d, want %d", tc.key, got, tc.want)
			}
			sum := 0
			for _, c := range r.Checks {
				sum += c.Points
			}
			if sum != r.Score {
				t.Fatalf("score %d != sum of checks %d", r.Score, sum)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	cases := []struct {
		pushed *time.Time
		want   Status
	}{
		{daysAgo(0), Active}, {daysAgo(30), Active}, {daysAgo(31), Slowing},
		{daysAgo(180), Slowing}, {daysAgo(181), Dormant}, {nil, Dormant},
	}
	for _, tc := range cases {
		if got := StatusFor(tc.pushed, now); got != tc.want {
			t.Errorf("StatusFor(%v) = %s, want %s", tc.pushed, got, tc.want)
		}
	}
}

func TestBreakdownDetails(t *testing.T) {
	in := perfect()
	in.Branches = append(in.Branches, Branch{Name: "x", LastCommitAt: daysAgo(100)})
	r := Evaluate(in, now)
	for _, c := range r.Checks {
		if c.Key == "stale_branches" && c.Detail != "1 stale branch (no commits in 90+ days)" {
			t.Fatalf("detail = %q", c.Detail)
		}
	}
}
