package stats

import (
	"testing"
	"time"
)

func TestExclusion(t *testing.T) {
	s := Settings{ExcludedRepoIDs: []int64{3}, ExcludedRepoGlobs: []string{"*-playground", "dotfiles"}}
	cases := []struct {
		id           int64
		name         string
		fork, arch   bool
		wantExcluded bool
	}{
		{1, "app", false, false, false},
		{3, "app2", false, false, true},
		{4, "Go-Playground", false, false, true}, // case-insensitive glob
		{5, "dotfiles", false, false, true},
		{6, "fork", true, false, true},
		{7, "old", false, true, true},
	}
	for _, c := range cases {
		if got := s.Exclusion(c.id, c.name, c.fork, c.arch) != ""; got != c.wantExcluded {
			t.Errorf("%s excluded=%v, want %v", c.name, got, c.wantExcluded)
		}
	}
	s.IncludeForks, s.IncludeArchived = true, true
	if s.Exclusion(6, "fork", true, false) != "" || s.Exclusion(7, "old", false, true) != "" {
		t.Error("include flags not honoured")
	}
}

func TestLanguages(t *testing.T) {
	rows := []RepoLanguage{
		{1, "Go", 600}, {1, "Shell", 100},
		{2, "TypeScript", 300}, {2, "Go", 0},
		{3, "Python", 5000}, // excluded repo
	}
	got := Languages(rows, map[int64]bool{1: true, 2: true})
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Language != "Go" || got[0].Bytes != 600 || got[0].Percent != 60 || got[0].Repos != 1 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Language != "TypeScript" || got[2].Language != "Shell" || got[2].Percent != 10 {
		t.Errorf("order = %+v", got)
	}
	var sum float64
	for _, s := range got {
		sum += s.Percent
	}
	if sum < 99.9 || sum > 100.1 {
		t.Errorf("percents sum to %v", sum)
	}
	if len(Languages(nil, nil)) != 0 {
		t.Error("empty input")
	}
}

func TestLanguagesTieBreakIsStable(t *testing.T) {
	got := Languages([]RepoLanguage{{1, "B", 10}, {1, "A", 10}}, map[int64]bool{1: true})
	if got[0].Language != "A" {
		t.Fatalf("ties should sort by name: %+v", got)
	}
}

func date(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC) }

func TestLanguageTrend(t *testing.T) {
	p := date(2025, 3, 1)
	spans := []RepoSpan{
		{1, date(2023, 6, 1), &p}, // active 2023..2025
		{2, date(2025, 1, 1), nil}, // never pushed: 2025 only
		{3, date(2024, 1, 1), &p},  // excluded
	}
	rows := []RepoLanguage{{1, "Go", 100}, {2, "TypeScript", 100}, {2, "CSS", 100}, {3, "Go", 999}}
	got := LanguageTrend(spans, rows, map[int64]bool{1: true, 2: true}, []string{"Go", "TypeScript"})
	if len(got) != 3 || got[0].Year != 2023 || got[2].Year != 2025 {
		t.Fatalf("years = %+v", got)
	}
	if got[0].Percents["Go"] != 100 || got[0].Repos != 1 {
		t.Errorf("2023 = %+v", got[0])
	}
	y25 := got[2]
	if y25.Repos != 2 || y25.Percents["Go"] != 33.33 || y25.Percents["TypeScript"] != 33.33 || y25.Percents["Other"] != 33.33 {
		t.Errorf("2025 = %+v", y25)
	}
}

func TestCommitsStreaksAndDistribution(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
	times := []time.Time{
		at("2026-11-01T20:00:00Z"), // Nov 2 01:30 IST (Monday)
		at("2026-11-02T10:00:00Z"), // Nov 2 15:30 IST
		at("2026-11-03T10:00:00Z"), // Nov 3
		at("2026-11-04T10:00:00Z"), // Nov 4  -> 3-day streak Nov 2..4
		at("2026-11-10T10:00:00Z"), // Nov 10
		at("2026-11-11T10:00:00Z"), // Nov 11 -> 2-day streak, ends "yesterday"
	}
	s := Commits(times, ist, at("2026-11-12T08:00:00Z"))
	if s.Total != 6 || s.ActiveDays != 5 {
		t.Fatalf("total=%d active=%d", s.Total, s.ActiveDays)
	}
	if s.LongestStreak != (Streak{Days: 3, Start: "2026-11-02", End: "2026-11-04"}) {
		t.Errorf("longest = %+v", s.LongestStreak)
	}
	if s.CurrentStreak != 2 {
		t.Errorf("current = %d", s.CurrentStreak)
	}
	if s.ByHour[1] != 1 || s.ByHour[15] != 5 || s.BusiestHour != 15 {
		t.Errorf("hours: 1=%d 15=%d busiest=%d", s.ByHour[1], s.ByHour[15], s.BusiestHour)
	}
	if s.ByWeekday[int(time.Monday)] != 2 || s.Heatmap[int(time.Monday)][1] != 1 {
		t.Errorf("weekday/heatmap wrong: %+v", s.ByWeekday)
	}
	// Two days later the streak is broken.
	if Commits(times, ist, at("2026-11-14T08:00:00Z")).CurrentStreak != 0 {
		t.Error("stale streak counted as current")
	}
}

func TestStreakAcrossMonthBoundary(t *testing.T) {
	s := Commits([]time.Time{date(2026, 1, 31), date(2026, 2, 1), date(2026, 2, 2)}, time.UTC, date(2026, 6, 1))
	if s.LongestStreak.Days != 3 {
		t.Fatalf("longest = %+v", s.LongestStreak)
	}
}

func TestWeekly(t *testing.T) {
	now := date(2026, 11, 17) // Tuesday; week starts Sunday Nov 15
	if WeekStart(now).Format(time.DateOnly) != "2026-11-15" {
		t.Fatalf("week start = %v", WeekStart(now))
	}
	pts := Weekly(map[string]int{"2026-11-08": 4}, []time.Time{date(2026, 11, 9), date(2026, 11, 16), date(2020, 1, 1)}, now, 3)
	if len(pts) != 3 || pts[0].Week != "2026-11-01" || pts[2].Week != "2026-11-15" {
		t.Fatalf("weeks = %+v", pts)
	}
	if pts[1].All != 4 || pts[1].Mine != 1 || pts[2].Mine != 1 || pts[0].Mine != 0 {
		t.Fatalf("values = %+v", pts)
	}
}
