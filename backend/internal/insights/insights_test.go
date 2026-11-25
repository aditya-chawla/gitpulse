package insights

import "testing"

func TestComputeInsights(t *testing.T) {
	res := Compute(RawRepoStats{
		RepoID:            1,
		FullName:          "org/active-lib",
		Source:            "watched",
		Status:            "active",
		HasCI:             true,
		HealthScore:       82,
		SampleCount:       10,
		SpanDays:          21,
		HealthSlopePerDay: -0.25,
		OpenPRs:           38,
		OpenPRsMean:       15,
		OpenPRsStdDev:     6,
		PRMedianAgeDays:   4.2,
		PRP90AgeDays:      11.5,
		PRStalePct:        12.0,
		CommitCount7d:     18,
		Commits7dMean:     16,
		Commits7dStdDev:   3,
		CommitCount30d:    65,
		UniqueAuthors30d:  12,
		TopAuthorLogin:    "alice",
		TopAuthorPct:      28,
		FailedRuns:        0,
	})

	if res.Responsiveness != "fast" {
		t.Fatalf("expected fast responsiveness, got %s", res.Responsiveness)
	}
	if res.DiversityLabel != "healthy" || res.DiversityScore < 0.6 {
		t.Fatalf("expected healthy diversity, got %s (%.2f)", res.DiversityLabel, res.DiversityScore)
	}
	if res.ProjectedHealth30d != 75 {
		t.Fatalf("expected projected health 75, got %d", res.ProjectedHealth30d)
	}
	if res.ReadinessVerdict != "Welcoming" || res.ContributorReadiness < 75 {
		t.Fatalf("expected Welcoming >=75, got %s (%d)", res.ReadinessVerdict, res.ContributorReadiness)
	}
	if len(res.Anomalies) != 1 || res.Anomalies[0].Metric != "open_prs" {
		t.Fatalf("expected open_prs spike anomaly, got %+v", res.Anomalies)
	}
}

func TestComputeConcentratedDormant(t *testing.T) {
	res := Compute(RawRepoStats{
		RepoID:           2,
		FullName:         "solo/old-tool",
		Source:           "watched",
		Status:           "dormant",
		HealthScore:      35,
		SampleCount:      1,
		OpenPRs:          6,
		PRMedianAgeDays:  45,
		PRStalePct:       83,
		CommitCount30d:   0,
		UniqueAuthors30d: 1,
		TopAuthorPct:     100,
	})

	if res.Responsiveness != "slow" {
		t.Fatalf("expected slow responsiveness, got %s", res.Responsiveness)
	}
	if res.DiversityLabel != "concentrated" || res.DiversityScore != 0 {
		t.Fatalf("expected concentrated 0 diversity, got %s (%.2f)", res.DiversityLabel, res.DiversityScore)
	}
	if res.ReadinessVerdict != "Dormant" {
		t.Fatalf("expected Dormant verdict, got %s", res.ReadinessVerdict)
	}
	if len(res.Anomalies) != 0 {
		t.Fatalf("expected no anomalies with sampleCount=1, got %+v", res.Anomalies)
	}
}
