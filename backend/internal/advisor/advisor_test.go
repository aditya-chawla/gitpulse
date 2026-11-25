package advisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
)

type fakeStats struct{}

func (fakeStats) Insights(_ context.Context, _ int64, source string) ([]insights.RepoInsights, error) {
	if source == "watched" {
		return []insights.RepoInsights{
			{
				RepoID:               2,
				FullName:             "oss/cool-lib",
				Source:               "watched",
				HealthScore:          84,
				SampleCount:          5,
				HealthSlopePerDay:    0.1,
				ProjectedHealth30d:   87,
				OpenPRs:              12,
				PRMedianAgeDays:      3.5,
				PRP90AgeDays:         9.0,
				PRStalePct:           8,
				Responsiveness:       "fast",
				CommitCount7d:        14,
				CommitCount30d:       50,
				UniqueAuthors30d:     11,
				TopAuthorPct:         25,
				DiversityScore:       0.85,
				DiversityLabel:       "healthy",
				ContributorReadiness: 88,
				ReadinessVerdict:     "Welcoming",
			},
		}, nil
	}
	return []insights.RepoInsights{
		{
			RepoID:             1,
			FullName:           "me/app",
			Source:             "owned",
			HealthScore:        62,
			SampleCount:        4,
			HealthSlopePerDay:  -0.3,
			ProjectedHealth30d: 53,
			Anomalies: []insights.Anomaly{
				{Metric: "open_prs", ZScore: 2.3, Summary: "Open PRs is +2.3σ above 30d baseline (14 vs avg 4.0)"},
			},
		},
	}, nil
}

func (fakeStats) RepoInsights(_ context.Context, _ int64, id int64) (*insights.RepoInsights, error) {
	if id == 2 {
		rs, _ := fakeStats{}.Insights(context.Background(), 1, "watched")
		return &rs[0], nil
	}
	return nil, stats.ErrNotFound
}

func (fakeStats) ActionItemsForSource(_ context.Context, _ int64, _ int, source string) ([]stats.ActionItem, error) {
	if source == "watched" {
		return nil, nil
	}
	return []stats.ActionItem{
		{ID: 10, RepoID: 1, RepoName: "me/app", Rule: "ci_rot", Severity: 4, Title: "CI failing on main", URL: "https://github.com/me/app/actions"},
	}, nil
}

func TestAskDeterministicBothRoles(t *testing.T) {
	svc := New(fakeStats{})

	mRes, err := svc.Ask(context.Background(), 1, AskRequest{Role: RoleMaintainer, Question: "What should I fix?"})
	if err != nil {
		t.Fatal(err)
	}
	if mRes.Mode != "deterministic" || mRes.Role != RoleMaintainer || !strings.Contains(mRes.Answer, "CI failing on main") {
		t.Fatalf("unexpected maintainer answer: %+v", mRes)
	}

	cRes, err := svc.Ask(context.Background(), 1, AskRequest{Role: RoleContributor, RepoID: 2, Question: "Should I contribute?"})
	if err != nil {
		t.Fatal(err)
	}
	if cRes.Mode != "deterministic" || cRes.Role != RoleContributor || !strings.Contains(cRes.Answer, "Welcoming") {
		t.Fatalf("unexpected contributor answer: %+v", cRes)
	}
}

func TestAskBYOKProvidersAndFallback(t *testing.T) {
	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "gem-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]string{{"text": "Gemini says: great repo to contribute to!"}}}},
			},
		})
	}))
	defer geminiSrv.Close()

	svc := New(fakeStats{})
	svc.geminiBaseURL = geminiSrv.URL

	res, err := svc.Ask(context.Background(), 1, AskRequest{
		Role:     RoleContributor,
		RepoID:   2,
		Question: "Should I contribute?",
		Provider: "gemini",
		APIKey:   "gem-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "gemini" || !strings.Contains(res.Answer, "Gemini says") {
		t.Fatalf("expected gemini response, got %+v", res)
	}

	// Bad key should gracefully fall back to deterministic with a warning.
	fb, err := svc.Ask(context.Background(), 1, AskRequest{
		Role:     RoleContributor,
		RepoID:   2,
		Question: "Should I contribute?",
		Provider: "gemini",
		APIKey:   "wrong-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fb.Mode != "deterministic" || fb.Warning == "" || !strings.Contains(fb.Answer, "Contributor Readiness") {
		t.Fatalf("expected fallback response with warning, got %+v", fb)
	}
}
