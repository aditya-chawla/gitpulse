// Package trends computes repository direction (improving/steady/slipping),
// historical deltas, and rule-based silent-decay alerts. It is pure (no I/O)
// so every rule is easy to test in isolation.
package trends

import (
	"fmt"
	"slices"
	"time"
)

type Direction string

const (
	Improving Direction = "improving"
	Steady    Direction = "steady"
	Slipping  Direction = "slipping"
)

// Snapshot mirrors one row of repo_trend_snapshots.
type Snapshot struct {
	RepoID           int64     `json:"repoId"`
	ScanID           int64     `json:"scanId"`
	ScannedAt        time.Time `json:"scannedAt"`
	HealthScore      int       `json:"healthScore"`
	OpenPRs          int       `json:"openPrs"`
	OpenIssues       int       `json:"openIssues"`
	StaleBranches    int       `json:"staleBranches"`
	FailedRuns       int       `json:"failedRuns"`
	CommitCount7d    int       `json:"commitCount7d"`
	CommitCount30d   int       `json:"commitCount30d"`
	UniqueAuthors30d int       `json:"uniqueAuthors30d"`
	TopAuthorLogin   string    `json:"topAuthorLogin,omitempty"`
	TopAuthorPct     int       `json:"topAuthorPct"`
	Stars            int       `json:"stars"`
	Forks            int       `json:"forks"`
}

type DeltaSet struct {
	HealthScore   int `json:"healthScore"`
	OpenPRs       int `json:"openPrs"`
	StaleBranches int `json:"staleBranches"`
	FailedRuns    int `json:"failedRuns"`
	CommitCadence int `json:"commitCadence"`
	UniqueAuthors int `json:"uniqueAuthors"`
}

type Alert struct {
	Rule     string         `json:"rule"`
	Severity int            `json:"severity"` // 1 (info) .. 5 (critical)
	Title    string         `json:"title"`
	Detail   map[string]any `json:"detail"`
	URL      string         `json:"url"`
}

type Result struct {
	Direction Direction `json:"direction"`
	Delta7d   *DeltaSet `json:"delta7d,omitempty"`
	Delta30d  *DeltaSet `json:"delta30d,omitempty"`
	Alerts    []Alert   `json:"alerts"`
}

type PRItem struct {
	Number    int
	Title     string
	URL       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RunItem struct {
	Workflow string
	Branch   string
	URL      string
}

// RepoContext carries current-scan metadata needed by decay rules.
type RepoContext struct {
	FullName              string
	DefaultBranch         string
	HasCI                 bool
	HasTests              bool
	IsFork                bool
	IsArchived            bool
	OpenPRs               []PRItem
	StaleBranchNames      []string
	FailedRuns            []RunItem
	HasTags               bool
	LastReleaseAt         *time.Time
	MergedPRsSinceRelease int
}

// Baseline holds the three historical snapshots used for comparison. Any of
// them may be nil when not enough scan history exists.
type Baseline struct {
	PrevScan *Snapshot
	Days7    *Snapshot
	Days30   *Snapshot
}

func delta(cur, base Snapshot) DeltaSet {
	return DeltaSet{
		HealthScore:   cur.HealthScore - base.HealthScore,
		OpenPRs:       cur.OpenPRs - base.OpenPRs,
		StaleBranches: cur.StaleBranches - base.StaleBranches,
		FailedRuns:    cur.FailedRuns - base.FailedRuns,
		CommitCadence: cur.CommitCount30d - base.CommitCount30d,
		UniqueAuthors: cur.UniqueAuthors30d - base.UniqueAuthors30d,
	}
}

// Evaluate computes 7d/30d deltas, triggered decay alerts, and overall direction.
func Evaluate(cur Snapshot, base Baseline, repo RepoContext, now time.Time) Result {
	res := Result{
		Direction: Steady,
		Alerts:    []Alert{},
	}
	if base.Days7 != nil {
		d := delta(cur, *base.Days7)
		res.Delta7d = &d
	}
	if base.Days30 != nil {
		d := delta(cur, *base.Days30)
		res.Delta30d = &d
	}

	if !repo.IsArchived {
		for _, fn := range []func(Snapshot, Baseline, RepoContext, time.Time) *Alert{
			rulePRBottleneck,
			ruleStaleBranches,
			ruleCIRot,
			ruleCommitCadenceDrop,
			ruleBusFactor,
			ruleUnprotectedCode,
			ruleNoRecentRelease,
		} {
			if a := fn(cur, base, repo, now); a != nil {
				res.Alerts = append(res.Alerts, *a)
			}
		}
	}

	// Pick the best available delta for direction: 30d -> 7d -> prevScan.
	var ref *DeltaSet
	switch {
	case res.Delta30d != nil:
		ref = res.Delta30d
	case res.Delta7d != nil:
		ref = res.Delta7d
	case base.PrevScan != nil:
		d := delta(cur, *base.PrevScan)
		ref = &d
	}

	highSeverity := false
	for _, a := range res.Alerts {
		if a.Severity >= 4 {
			highSeverity = true
			break
		}
	}

	switch {
	case highSeverity || (ref != nil && ref.HealthScore < 0):
		res.Direction = Slipping
	case ref != nil && ref.HealthScore > 0 && (ref.OpenPRs < 0 || ref.StaleBranches < 0 || ref.FailedRuns < 0):
		res.Direction = Improving
	default:
		res.Direction = Steady
	}
	return res
}

func repoURL(fullName, suffix string) string {
	return "https://github.com/" + fullName + suffix
}

func rulePRBottleneck(cur Snapshot, base Baseline, repo RepoContext, now time.Time) *Alert {
	const agingThreshold = 14 * 24 * time.Hour
	var aging []map[string]any
	for _, pr := range repo.OpenPRs {
		if now.Sub(pr.UpdatedAt) > agingThreshold {
			aging = append(aging, map[string]any{
				"number":  pr.Number,
				"ageDays": int(now.Sub(pr.UpdatedAt).Hours() / 24),
			})
		}
	}
	grew50Pct := base.Days30 != nil && base.Days30.OpenPRs > 0 && cur.OpenPRs*100 > base.Days30.OpenPRs*150

	if len(aging) < 3 && !grew50Pct {
		return nil
	}

	title := fmt.Sprintf("%d open PRs untouched >14 days on %s", len(aging), repo.FullName)
	if len(aging) < 3 && grew50Pct {
		title = fmt.Sprintf("Open PR backlog grew >50%% (%d → %d) on %s", base.Days30.OpenPRs, cur.OpenPRs, repo.FullName)
	}
	return &Alert{
		Rule:     "pr_bottleneck",
		Severity: 4,
		Title:    title,
		Detail: map[string]any{
			"agingCount": len(aging),
			"agingPrs":   aging,
			"openPrs":    cur.OpenPRs,
		},
		URL: repoURL(repo.FullName, "/pulls"),
	}
}

func ruleStaleBranches(cur Snapshot, base Baseline, repo RepoContext, _ time.Time) *Alert {
	increased := base.PrevScan != nil && cur.StaleBranches > base.PrevScan.StaleBranches
	if !increased && cur.StaleBranches < 5 {
		return nil
	}
	branches := repo.StaleBranchNames
	if len(branches) > 10 {
		branches = branches[:10]
	}
	return &Alert{
		Rule:     "stale_branches",
		Severity: 3,
		Title:    fmt.Sprintf("%d stale branches (>90 days) on %s", cur.StaleBranches, repo.FullName),
		Detail: map[string]any{
			"count":    cur.StaleBranches,
			"branches": slices.Clone(branches),
		},
		URL: repoURL(repo.FullName, "/branches"),
	}
}

func ruleCIRot(cur Snapshot, base Baseline, repo RepoContext, _ time.Time) *Alert {
	if base.PrevScan == nil || base.PrevScan.FailedRuns == 0 || cur.FailedRuns == 0 {
		return nil
	}
	var defaultRuns []RunItem
	for _, r := range repo.FailedRuns {
		if repo.DefaultBranch == "" || r.Branch == "" || r.Branch == repo.DefaultBranch {
			defaultRuns = append(defaultRuns, r)
		}
	}
	if len(defaultRuns) == 0 {
		return nil
	}
	names := make([]string, 0, len(defaultRuns))
	seen := map[string]bool{}
	for _, r := range defaultRuns {
		if !seen[r.Workflow] {
			seen[r.Workflow] = true
			names = append(names, r.Workflow)
		}
	}
	url := defaultRuns[0].URL
	if url == "" {
		url = repoURL(repo.FullName, "/actions")
	}
	return &Alert{
		Rule:     "ci_rot",
		Severity: 4,
		Title:    fmt.Sprintf("CI failing across consecutive scans on %s (%s)", repo.FullName, names[0]),
		Detail: map[string]any{
			"workflows":  names,
			"failedRuns": len(defaultRuns),
		},
		URL: url,
	}
}

func ruleCommitCadenceDrop(cur Snapshot, base Baseline, repo RepoContext, _ time.Time) *Alert {
	if base.Days30 == nil || base.Days30.CommitCount30d < 5 {
		return nil
	}
	if cur.CommitCount30d*100 >= base.Days30.CommitCount30d*60 {
		return nil
	}
	dropPct := 100 - (cur.CommitCount30d*100)/base.Days30.CommitCount30d
	return &Alert{
		Rule:     "commit_cadence_drop",
		Severity: 3,
		Title:    fmt.Sprintf("30-day commit cadence dropped %d%% (%d → %d) on %s", dropPct, base.Days30.CommitCount30d, cur.CommitCount30d, repo.FullName),
		Detail: map[string]any{
			"previous30d": base.Days30.CommitCount30d,
			"current30d":  cur.CommitCount30d,
			"dropPct":     dropPct,
		},
		URL: repoURL(repo.FullName, "/graphs/commit-activity"),
	}
}

func ruleBusFactor(cur Snapshot, _ Baseline, repo RepoContext, _ time.Time) *Alert {
	if cur.CommitCount30d < 10 || cur.TopAuthorPct <= 80 || cur.TopAuthorLogin == "" {
		return nil
	}
	return &Alert{
		Rule:     "bus_factor",
		Severity: 4,
		Title:    fmt.Sprintf("Bus factor risk: %s authored %d%% of recent commits on %s", cur.TopAuthorLogin, cur.TopAuthorPct, repo.FullName),
		Detail: map[string]any{
			"topAuthor":    cur.TopAuthorLogin,
			"topAuthorPct": cur.TopAuthorPct,
			"commits30d":   cur.CommitCount30d,
		},
		URL: repoURL(repo.FullName, "/graphs/contributors"),
	}
}

func ruleUnprotectedCode(cur Snapshot, _ Baseline, repo RepoContext, _ time.Time) *Alert {
	if cur.CommitCount7d < 5 || repo.HasCI || repo.HasTests {
		return nil
	}
	return &Alert{
		Rule:     "unprotected_code",
		Severity: 5,
		Title:    fmt.Sprintf("Active repo %s (%d commits/wk) has no CI or tests", repo.FullName, cur.CommitCount7d),
		Detail: map[string]any{
			"commits7d": cur.CommitCount7d,
			"hasCi":     repo.HasCI,
			"hasTests":  repo.HasTests,
		},
		URL: repoURL(repo.FullName, ""),
	}
}

func ruleNoRecentRelease(_ Snapshot, _ Baseline, repo RepoContext, now time.Time) *Alert {
	if !repo.HasTags || repo.LastReleaseAt == nil {
		return nil
	}
	if now.Sub(*repo.LastReleaseAt) <= 90*24*time.Hour || repo.MergedPRsSinceRelease < 20 {
		return nil
	}
	days := int(now.Sub(*repo.LastReleaseAt).Hours() / 24)
	return &Alert{
		Rule:     "no_recent_release",
		Severity: 2,
		Title:    fmt.Sprintf("No release in %d days (%d merged PRs waiting) on %s", days, repo.MergedPRsSinceRelease, repo.FullName),
		Detail: map[string]any{
			"daysSinceRelease":      days,
			"mergedPrsSinceRelease": repo.MergedPRsSinceRelease,
		},
		URL: repoURL(repo.FullName, "/releases"),
	}
}
