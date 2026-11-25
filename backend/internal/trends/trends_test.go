package trends

import (
	"testing"
	"time"
)

func TestEvaluateDirectionAndRules(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	baseRepo := RepoContext{
		FullName:      "org/app",
		DefaultBranch: "main",
		HasCI:         true,
		HasTests:      true,
	}

	t.Run("steady when no baseline and no alerts", func(t *testing.T) {
		res := Evaluate(Snapshot{HealthScore: 90}, Baseline{}, baseRepo, now)
		if res.Direction != Steady {
			t.Fatalf("direction = %s, want steady", res.Direction)
		}
		if len(res.Alerts) != 0 {
			t.Fatalf("alerts = %+v, want none", res.Alerts)
		}
	})

	t.Run("improving when health up and open PRs down", func(t *testing.T) {
		cur := Snapshot{HealthScore: 85, OpenPRs: 2, StaleBranches: 1}
		b30 := &Snapshot{HealthScore: 75, OpenPRs: 6, StaleBranches: 1}
		res := Evaluate(cur, Baseline{Days30: b30}, baseRepo, now)
		if res.Direction != Improving {
			t.Fatalf("direction = %s, want improving", res.Direction)
		}
		if res.Delta30d == nil || res.Delta30d.HealthScore != 10 || res.Delta30d.OpenPRs != -4 {
			t.Fatalf("unexpected delta30d: %+v", res.Delta30d)
		}
	})

	t.Run("pr_bottleneck triggers on 3+ aging PRs and sets slipping", func(t *testing.T) {
		old := now.Add(-20 * 24 * time.Hour)
		repo := baseRepo
		repo.OpenPRs = []PRItem{
			{Number: 1, UpdatedAt: old},
			{Number: 2, UpdatedAt: old},
			{Number: 3, UpdatedAt: old},
		}
		res := Evaluate(Snapshot{HealthScore: 80, OpenPRs: 3}, Baseline{}, repo, now)
		if res.Direction != Slipping {
			t.Fatalf("direction = %s, want slipping", res.Direction)
		}
		if !hasRule(res.Alerts, "pr_bottleneck") {
			t.Fatalf("expected pr_bottleneck alert, got %+v", res.Alerts)
		}
	})

	t.Run("pr_bottleneck triggers on >50% 30d growth", func(t *testing.T) {
		res := Evaluate(
			Snapshot{HealthScore: 80, OpenPRs: 7},
			Baseline{Days30: &Snapshot{HealthScore: 80, OpenPRs: 4}},
			baseRepo, now,
		)
		if !hasRule(res.Alerts, "pr_bottleneck") {
			t.Fatalf("expected pr_bottleneck on >50%% growth, got %+v", res.Alerts)
		}
	})

	t.Run("stale_branches triggers on increase or >=5", func(t *testing.T) {
		repo := baseRepo
		repo.StaleBranchNames = []string{"a", "b"}
		res := Evaluate(
			Snapshot{HealthScore: 80, StaleBranches: 2},
			Baseline{PrevScan: &Snapshot{HealthScore: 80, StaleBranches: 1}},
			repo, now,
		)
		if !hasRule(res.Alerts, "stale_branches") {
			t.Fatalf("expected stale_branches alert on increase")
		}
	})

	t.Run("ci_rot triggers on consecutive failed runs on default branch", func(t *testing.T) {
		repo := baseRepo
		repo.FailedRuns = []RunItem{{Workflow: "CI", Branch: "main", URL: "https://github.com/org/app/actions/runs/1"}}
		res := Evaluate(
			Snapshot{HealthScore: 80, FailedRuns: 1},
			Baseline{PrevScan: &Snapshot{HealthScore: 80, FailedRuns: 2}},
			repo, now,
		)
		if !hasRule(res.Alerts, "ci_rot") || res.Direction != Slipping {
			t.Fatalf("expected ci_rot + slipping, got dir=%s alerts=%+v", res.Direction, res.Alerts)
		}
	})

	t.Run("commit_cadence_drop triggers on >40% drop", func(t *testing.T) {
		res := Evaluate(
			Snapshot{HealthScore: 80, CommitCount30d: 10},
			Baseline{Days30: &Snapshot{HealthScore: 80, CommitCount30d: 30}},
			baseRepo, now,
		)
		if !hasRule(res.Alerts, "commit_cadence_drop") {
			t.Fatalf("expected commit_cadence_drop alert, got %+v", res.Alerts)
		}
	})

	t.Run("bus_factor triggers when >80% and >=10 commits", func(t *testing.T) {
		res := Evaluate(
			Snapshot{HealthScore: 80, CommitCount30d: 20, TopAuthorLogin: "alice", TopAuthorPct: 90},
			Baseline{}, baseRepo, now,
		)
		if !hasRule(res.Alerts, "bus_factor") || res.Direction != Slipping {
			t.Fatalf("expected bus_factor + slipping, got %+v", res)
		}
	})

	t.Run("unprotected_code triggers when >=5 weekly commits without CI and tests", func(t *testing.T) {
		repo := baseRepo
		repo.HasCI = false
		repo.HasTests = false
		res := Evaluate(Snapshot{HealthScore: 55, CommitCount7d: 6}, Baseline{}, repo, now)
		if !hasRule(res.Alerts, "unprotected_code") || res.Direction != Slipping {
			t.Fatalf("expected unprotected_code + slipping, got %+v", res)
		}
	})

	t.Run("no_recent_release triggers when >90d and >=20 merged PRs", func(t *testing.T) {
		lastRel := now.Add(-120 * 24 * time.Hour)
		repo := baseRepo
		repo.HasTags = true
		repo.LastReleaseAt = &lastRel
		repo.MergedPRsSinceRelease = 25
		res := Evaluate(Snapshot{HealthScore: 90}, Baseline{}, repo, now)
		if !hasRule(res.Alerts, "no_recent_release") {
			t.Fatalf("expected no_recent_release alert, got %+v", res.Alerts)
		}
	})

	t.Run("archived repos suppress all alerts", func(t *testing.T) {
		repo := baseRepo
		repo.IsArchived = true
		repo.HasCI = false
		repo.HasTests = false
		res := Evaluate(Snapshot{HealthScore: 40, CommitCount7d: 10}, Baseline{}, repo, now)
		if len(res.Alerts) != 0 {
			t.Fatalf("expected no alerts for archived repo, got %+v", res.Alerts)
		}
	})
}

func hasRule(alerts []Alert, rule string) bool {
	for _, a := range alerts {
		if a.Rule == rule {
			return true
		}
	}
	return false
}
