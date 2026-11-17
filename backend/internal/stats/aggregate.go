// Package stats turns stored snapshots into dashboard data. Aggregations are
// pure functions (tested in isolation); queries.go loads their inputs.
package stats

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"time"
)

type Settings struct {
	ExcludedRepoIDs   []int64  `json:"excludedRepoIds"`
	ExcludedRepoGlobs []string `json:"excludedRepoGlobs"`
	IncludeForks      bool     `json:"includeForks"`
	IncludeArchived   bool     `json:"includeArchived"`
	Timezone          string   `json:"timezone"`
}

// Exclusion reports whether a repo is left out of aggregates, and why.
func (s Settings) Exclusion(id int64, name string, isFork, isArchived bool) string {
	switch {
	case slices.Contains(s.ExcludedRepoIDs, id):
		return "excluded in settings"
	case isFork && !s.IncludeForks:
		return "fork"
	case isArchived && !s.IncludeArchived:
		return "archived"
	}
	for _, g := range s.ExcludedRepoGlobs {
		if ok, _ := path.Match(strings.ToLower(g), strings.ToLower(name)); ok {
			return "matches pattern " + g
		}
	}
	return ""
}

type RepoLanguage struct {
	RepoID   int64
	Language string
	Bytes    int64
}

type LanguageShare struct {
	Language string  `json:"language"`
	Bytes    int64   `json:"bytes"`
	Percent  float64 `json:"percent"`
	Repos    int     `json:"repos"`
}

// Languages sums bytes per language over included repos, largest first.
func Languages(rows []RepoLanguage, included map[int64]bool) []LanguageShare {
	bytes := map[string]int64{}
	repos := map[string]int{}
	var total int64
	for _, r := range rows {
		if !included[r.RepoID] || r.Bytes <= 0 {
			continue
		}
		bytes[r.Language] += r.Bytes
		repos[r.Language]++
		total += r.Bytes
	}
	out := make([]LanguageShare, 0, len(bytes))
	for l, b := range bytes {
		out = append(out, LanguageShare{Language: l, Bytes: b, Repos: repos[l], Percent: pct(b, total)})
	}
	slices.SortFunc(out, func(a, b LanguageShare) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(a.Language, b.Language))
	})
	return out
}

func pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(int64(float64(part)*10000/float64(total)+0.5)) / 100 // 2 decimals
}

// TopLanguages returns the n largest language names from a sorted share list.
func TopLanguages(shares []LanguageShare, n int) []string {
	out := []string{}
	for i := 0; i < len(shares) && i < n; i++ {
		out = append(out, shares[i].Language)
	}
	return out
}

type RepoSpan struct {
	RepoID    int64
	CreatedAt time.Time
	PushedAt  *time.Time
}

// YearShare is one year of the language trend: percent of bytes per language
// among repos active that year. Languages outside `keep` are folded into Other.
type YearShare struct {
	Year     int                `json:"year"`
	Repos    int                `json:"repos"`
	Percents map[string]float64 `json:"percents"`
}

// LanguageTrend attributes each repo's bytes to every calendar year from its
// creation to its last push (both inclusive).
func LanguageTrend(spans []RepoSpan, rows []RepoLanguage, included map[int64]bool, keep []string) []YearShare {
	byRepo := map[int64][]RepoLanguage{}
	for _, r := range rows {
		byRepo[r.RepoID] = append(byRepo[r.RepoID], r)
	}
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	type acc struct {
		repos int
		bytes map[string]int64
		total int64
	}
	years := map[int]*acc{}
	for _, s := range spans {
		if !included[s.RepoID] {
			continue
		}
		from, to := s.CreatedAt.Year(), s.CreatedAt.Year()
		if s.PushedAt != nil && s.PushedAt.Year() > to {
			to = s.PushedAt.Year()
		}
		for y := from; y <= to; y++ {
			a := years[y]
			if a == nil {
				a = &acc{bytes: map[string]int64{}}
				years[y] = a
			}
			a.repos++
			for _, l := range byRepo[s.RepoID] {
				name := l.Language
				if !kept[name] {
					name = "Other"
				}
				a.bytes[name] += l.Bytes
				a.total += l.Bytes
			}
		}
	}
	out := make([]YearShare, 0, len(years))
	for y, a := range years {
		ys := YearShare{Year: y, Repos: a.repos, Percents: map[string]float64{}}
		for l, b := range a.bytes {
			ys.Percents[l] = pct(b, a.total)
		}
		out = append(out, ys)
	}
	slices.SortFunc(out, func(a, b YearShare) int { return cmp.Compare(a.Year, b.Year) })
	return out
}

type CommitStats struct {
	Total          int        `json:"total"`
	ByWeekday      [7]int     `json:"byWeekday"` // Sunday..Saturday
	ByHour         [24]int    `json:"byHour"`
	Heatmap        [7][24]int `json:"heatmap"` // [weekday][hour]
	LongestStreak  Streak     `json:"longestStreak"`
	CurrentStreak  int        `json:"currentStreak"`
	ActiveDays     int        `json:"activeDays"`
	BusiestWeekday int        `json:"busiestWeekday"`
	BusiestHour    int        `json:"busiestHour"`
}

type Streak struct {
	Days  int    `json:"days"`
	Start string `json:"start,omitempty"` // YYYY-MM-DD
	End   string `json:"end,omitempty"`
}

// Commits computes day/hour distributions and streaks in the given location.
// `today` decides whether the current streak is still alive (a streak counts
// as current if its last day is today or yesterday).
func Commits(times []time.Time, loc *time.Location, today time.Time) CommitStats {
	var s CommitStats
	days := map[string]bool{}
	for _, t := range times {
		lt := t.In(loc)
		wd, h := int(lt.Weekday()), lt.Hour()
		s.Total++
		s.ByWeekday[wd]++
		s.ByHour[h]++
		s.Heatmap[wd][h]++
		days[lt.Format(time.DateOnly)] = true
	}
	s.ActiveDays = len(days)
	s.BusiestWeekday = argmax(s.ByWeekday[:])
	s.BusiestHour = argmax(s.ByHour[:])

	sorted := make([]string, 0, len(days))
	for d := range days {
		sorted = append(sorted, d)
	}
	slices.Sort(sorted)
	var run Streak
	var prev time.Time
	for _, d := range sorted {
		t, _ := time.ParseInLocation(time.DateOnly, d, loc)
		if run.Days > 0 && sameDay(prev.AddDate(0, 0, 1), t) {
			run.Days++
			run.End = d
		} else {
			run = Streak{Days: 1, Start: d, End: d}
		}
		if run.Days > s.LongestStreak.Days {
			s.LongestStreak = run
		}
		prev = t
	}
	if run.Days > 0 {
		lt := today.In(loc)
		td := lt.Format(time.DateOnly)
		yd := lt.AddDate(0, 0, -1).Format(time.DateOnly)
		if run.End == td || run.End == yd {
			s.CurrentStreak = run.Days
		}
	}
	return s
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func argmax(v []int) int {
	best := 0
	for i := range v {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

type WeekPoint struct {
	Week string `json:"week"` // YYYY-MM-DD, Sunday
	All  int    `json:"all"`  // all authors, from GitHub commit activity
	Mine int    `json:"mine"` // my default-branch commits
}

// WeekStart returns the Sunday (00:00 UTC) starting t's week, matching
// GitHub's commit_activity buckets.
func WeekStart(t time.Time) time.Time {
	t = t.UTC()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -int(d.Weekday()))
}

// Weekly builds the last `weeks` weeks ending with the week containing now.
func Weekly(all map[string]int, mine []time.Time, now time.Time, weeks int) []WeekPoint {
	m := map[string]int{}
	for _, t := range mine {
		m[WeekStart(t).Format(time.DateOnly)]++
	}
	start := WeekStart(now).AddDate(0, 0, -7*(weeks-1))
	out := make([]WeekPoint, weeks)
	for i := range weeks {
		w := start.AddDate(0, 0, 7*i).Format(time.DateOnly)
		out[i] = WeekPoint{Week: w, All: all[w], Mine: m[w]}
	}
	return out
}
