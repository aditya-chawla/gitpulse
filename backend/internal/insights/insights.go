// Package insights computes statistical and data-science signals from
// repository snapshots (Z-score anomalies, PR age percentiles, health
// trajectory linear regression, contributor diversity, and readiness).
package insights

import (
	"fmt"
	"math"
)

// Anomaly describes a statistically unusual metric relative to the repo's own history.
type Anomaly struct {
	Metric  string  `json:"metric"`
	ZScore  float64 `json:"zScore"`
	Current float64 `json:"current"`
	Mean    float64 `json:"mean"`
	StdDev  float64 `json:"stdDev"`
	Summary string  `json:"summary"`
}

// RepoInsights holds the deterministic data-science metrics for one repository.
type RepoInsights struct {
	RepoID               int64     `json:"repoId"`
	FullName             string    `json:"fullName"`
	Source               string    `json:"source"`
	HealthScore          int       `json:"healthScore"`
	SampleCount          int       `json:"sampleCount"`
	HealthSlopePerDay    float64   `json:"healthSlopePerDay"`
	ProjectedHealth30d   int       `json:"projectedHealth30d"`
	OpenPRs              int       `json:"openPrs"`
	PRMedianAgeDays      float64   `json:"prMedianAgeDays"`
	PRP90AgeDays         float64   `json:"prP90AgeDays"`
	PRStalePct           float64   `json:"prStalePct"`
	Responsiveness       string    `json:"responsiveness"` // none | fast | moderate | slow
	CommitCount7d        int       `json:"commitCount7d"`
	CommitCount30d       int       `json:"commitCount30d"`
	UniqueAuthors30d     int       `json:"uniqueAuthors30d"`
	TopAuthorLogin       string    `json:"topAuthorLogin,omitempty"`
	TopAuthorPct         int       `json:"topAuthorPct"`
	DiversityScore       float64   `json:"diversityScore"` // 0.0 (concentrated) to 1.0 (well distributed)
	DiversityLabel       string    `json:"diversityLabel"` // inactive | concentrated | moderate | healthy
	ContributorReadiness int       `json:"contributorReadiness"`
	ReadinessVerdict     string    `json:"readinessVerdict"`
	Anomalies            []Anomaly `json:"anomalies"`
}

// RawRepoStats contains the raw SQL aggregates computed by Postgres.
type RawRepoStats struct {
	RepoID            int64
	FullName          string
	Source            string
	Status            string
	HasCI             bool
	HealthScore       int
	SampleCount       int
	SpanDays          float64
	HealthSlopePerDay float64
	OpenPRs           int
	OpenPRsMean       float64
	OpenPRsStdDev     float64
	PRMedianAgeDays   float64
	PRP90AgeDays      float64
	PRStalePct        float64
	CommitCount7d     int
	Commits7dMean     float64
	Commits7dStdDev   float64
	CommitCount30d    int
	UniqueAuthors30d  int
	TopAuthorLogin    string
	TopAuthorPct      int
	FailedRuns        int
	FailedRunsMean    float64
	FailedRunsStdDev  float64
}

// Compute derives scores, labels, projections, and Z-score anomalies from raw Postgres aggregates.
func Compute(r RawRepoStats) RepoInsights {
	out := RepoInsights{
		RepoID:            r.RepoID,
		FullName:          r.FullName,
		Source:            r.Source,
		HealthScore:       r.HealthScore,
		SampleCount:       r.SampleCount,
		HealthSlopePerDay: round2(r.HealthSlopePerDay),
		OpenPRs:           r.OpenPRs,
		PRMedianAgeDays:   round1(r.PRMedianAgeDays),
		PRP90AgeDays:      round1(r.PRP90AgeDays),
		PRStalePct:        round1(r.PRStalePct),
		CommitCount7d:     r.CommitCount7d,
		CommitCount30d:    r.CommitCount30d,
		UniqueAuthors30d:  r.UniqueAuthors30d,
		TopAuthorLogin:    r.TopAuthorLogin,
		TopAuthorPct:      r.TopAuthorPct,
		Anomalies:         []Anomaly{},
	}

	// Project 30-day health score only when there are >=2 snapshots spanning >=1 day.
	if r.SampleCount >= 2 && r.SpanDays >= 1.0 {
		proj := int(math.Round(float64(r.HealthScore) + r.HealthSlopePerDay*30.0))
		out.ProjectedHealth30d = clampInt(proj, 0, 100)
	} else {
		out.ProjectedHealth30d = r.HealthScore
		out.HealthSlopePerDay = 0
	}

	out.Responsiveness = classifyResponsiveness(r.OpenPRs, r.PRMedianAgeDays, r.PRStalePct)
	out.DiversityScore, out.DiversityLabel = computeDiversity(r.UniqueAuthors30d, r.TopAuthorPct)
	out.ContributorReadiness, out.ReadinessVerdict = computeReadiness(r, out.DiversityScore, out.Responsiveness)

	// Z-score anomalies require at least 3 historical snapshots.
	if r.SampleCount >= 3 {
		if a, ok := checkAnomaly("open_prs", float64(r.OpenPRs), r.OpenPRsMean, r.OpenPRsStdDev, 1.5, true); ok {
			out.Anomalies = append(out.Anomalies, a)
		}
		if a, ok := checkAnomaly("commit_count_7d", float64(r.CommitCount7d), r.Commits7dMean, r.Commits7dStdDev, 1.5, false); ok {
			out.Anomalies = append(out.Anomalies, a)
		}
		if a, ok := checkAnomaly("failed_runs", float64(r.FailedRuns), r.FailedRunsMean, r.FailedRunsStdDev, 1.5, true); ok {
			out.Anomalies = append(out.Anomalies, a)
		}
	}

	return out
}

func classifyResponsiveness(openPRs int, medianDays, stalePct float64) string {
	if openPRs == 0 {
		return "none"
	}
	if medianDays < 5 && stalePct < 25 {
		return "fast"
	}
	if medianDays < 14 && stalePct < 50 {
		return "moderate"
	}
	return "slow"
}

func computeDiversity(authors, topPct int) (float64, string) {
	if authors <= 0 {
		return 0, "inactive"
	}
	if authors == 1 || topPct >= 100 {
		return 0, "concentrated"
	}
	// Combine author breadth (log scale up to 10 authors) with non-top-author share.
	breadth := math.Min(1.0, math.Log(float64(authors))/math.Log(10.0))
	spread := math.Max(0.0, 1.0-float64(topPct)/100.0)
	score := round2(0.4*breadth + 0.6*spread)
	switch {
	case score >= 0.60:
		return score, "healthy"
	case score >= 0.30:
		return score, "moderate"
	default:
		return score, "concentrated"
	}
}

func computeReadiness(r RawRepoStats, diversity float64, responsiveness string) (int, string) {
	healthPts := float64(r.HealthScore) * 0.35

	var momentumPts float64
	switch {
	case r.CommitCount30d >= 20:
		momentumPts = 20
	case r.CommitCount30d >= 5:
		momentumPts = 14
	case r.CommitCount30d >= 1:
		momentumPts = 7
	}

	diversityPts := diversity * 20.0

	var prPts float64
	switch responsiveness {
	case "none", "fast":
		prPts = 15
	case "moderate":
		prPts = 10
	case "slow":
		prPts = 3
	}

	var ciPts float64
	if r.HasCI && r.FailedRuns == 0 {
		ciPts = 10
	} else if r.HasCI {
		ciPts = 4
	}

	score := clampInt(int(math.Round(healthPts+momentumPts+diversityPts+prPts+ciPts)), 0, 100)
	switch {
	case r.Status == "dormant" || (r.CommitCount30d == 0 && r.HealthScore < 45):
		return score, "Dormant"
	case score >= 75:
		return score, "Welcoming"
	case score >= 55 && responsiveness == "slow":
		return score, "Active but backlogged"
	case score >= 55:
		return score, "Steady"
	default:
		return score, "Caution"
	}
}

func checkAnomaly(metric string, cur, mean, std, threshold float64, highOnly bool) (Anomaly, bool) {
	if std <= 0.001 {
		return Anomaly{}, false
	}
	z := (cur - mean) / std
	if highOnly && z < threshold {
		return Anomaly{}, false
	}
	if !highOnly && math.Abs(z) < threshold {
		return Anomaly{}, false
	}
	dir := "above"
	if z < 0 {
		dir = "below"
	}
	return Anomaly{
		Metric:  metric,
		ZScore:  round2(z),
		Current: round1(cur),
		Mean:    round1(mean),
		StdDev:  round1(std),
		Summary: fmt.Sprintf("%s is %+.1fσ %s 30d baseline (%.0f vs avg %.1f)", metricLabel(metric), z, dir, cur, mean),
	}, true
}

func metricLabel(m string) string {
	switch m {
	case "open_prs":
		return "Open PRs"
	case "commit_count_7d":
		return "Weekly commits"
	case "failed_runs":
		return "Failed CI runs"
	default:
		return m
	}
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
