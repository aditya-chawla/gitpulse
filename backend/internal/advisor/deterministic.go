package advisor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
)

func buildDeterministic(role, question string, repos []insights.RepoInsights, actions []stats.ActionItem) *AskResponse {
	links := collectLinks(repos, actions)
	if len(repos) == 0 {
		msg := "No repositories found for this view yet. Add a repository or run a scan to generate insights."
		if role == RoleContributor {
			msg = "No scanned watched repositories yet. Add a public `owner/repo` on the Watched page and run a scan to evaluate its contributor readiness."
		}
		return &AskResponse{
			Answer: msg,
			Mode:   "deterministic",
			Role:   role,
			Links:  links,
		}
	}

	if len(repos) == 1 {
		return &AskResponse{
			Answer: explainSingleRepo(role, question, repos[0], actions),
			Mode:   "deterministic",
			Role:   role,
			Links:  links,
		}
	}

	if role == RoleContributor {
		return &AskResponse{
			Answer: explainWatchedPortfolio(question, repos, actions),
			Mode:   "deterministic",
			Role:   role,
			Links:  links,
		}
	}
	return &AskResponse{
		Answer: explainMaintainerPortfolio(question, repos, actions),
		Mode:   "deterministic",
		Role:   role,
		Links:  links,
	}
}

func explainSingleRepo(role, question string, r insights.RepoInsights, actions []stats.ActionItem) string {
	var sb strings.Builder
	if role == RoleContributor {
		fmt.Fprintf(&sb, "**%s — Contributor Readiness: %d/100 (%s)**\n\n", r.FullName, r.ContributorReadiness, r.ReadinessVerdict)
	} else {
		fmt.Fprintf(&sb, "**%s — Health: %d/100** (30d projection: %d/100)\n\n", r.FullName, r.HealthScore, r.ProjectedHealth30d)
	}

	fmt.Fprintf(&sb, "- **Activity & Trajectory:** %d commits (7d) · %d commits (30d). ", r.CommitCount7d, r.CommitCount30d)
	if r.SampleCount >= 2 && r.HealthSlopePerDay != 0 {
		fmt.Fprintf(&sb, "Health slope is **%+.2f pts/day** (30d projection: **%d/100**).\n", r.HealthSlopePerDay, r.ProjectedHealth30d)
	} else {
		sb.WriteString("Run more scans over time to unlock 30-day linear trajectory forecasting.\n")
	}

	fmt.Fprintf(&sb, "- **Contributor Spread (Bus Factor):** %d unique authors in 30d (diversity **%.2f · %s**", r.UniqueAuthors30d, r.DiversityScore, r.DiversityLabel)
	if r.TopAuthorPct > 0 {
		fmt.Fprintf(&sb, ", top author %d%%", r.TopAuthorPct)
	}
	sb.WriteString(").\n")

	if r.OpenPRs > 0 {
		fmt.Fprintf(&sb, "- **PR Responsiveness (%s):** %d open PRs · median age **%.1fd** · P90 **%.1fd** · **%.0f%%** untouched >14d.\n",
			r.Responsiveness, r.OpenPRs, r.PRMedianAgeDays, r.PRP90AgeDays, r.PRStalePct)
	} else {
		sb.WriteString("- **PR Responsiveness:** No open PR backlog right now.\n")
	}

	if len(r.Anomalies) > 0 {
		sb.WriteString("\n**Statistical Anomalies (Z-score):**\n")
		for _, a := range r.Anomalies {
			fmt.Fprintf(&sb, "- %s\n", a.Summary)
		}
	}

	if len(actions) > 0 {
		label := "**Top Maintenance Priorities:**"
		if role == RoleContributor {
			label = "**Observed Project Signals (read-only):**"
		}
		fmt.Fprintf(&sb, "\n%s\n", label)
		for i, a := range actions {
			if i >= 4 {
				break
			}
			fmt.Fprintf(&sb, "- %s (Severity %d/5)\n", a.Title, a.Severity)
		}
	}

	sb.WriteString("\n**Recommendation:** ")
	if role == RoleContributor {
		switch {
		case r.ReadinessVerdict == "Welcoming":
			sb.WriteString("Strong candidate for contributing — active commits, healthy contributor spread, and manageable PR turnaround.")
		case r.Responsiveness == "slow":
			sb.WriteString("Active codebase, but PR reviews are backlogged. Start with a well-scoped issue or small bugfix and check recent merged PRs first.")
		case r.DiversityLabel == "concentrated":
			sb.WriteString("Relies heavily on a single maintainer; open an issue to confirm direction before investing in a large PR.")
		case r.ReadinessVerdict == "Dormant":
			sb.WriteString("Appears dormant with minimal recent commit activity — avoid time-sensitive contributions unless you plan to fork.")
		default:
			sb.WriteString("Moderately active project. Review open issues and CI status before picking up a task.")
		}
	} else {
		switch {
		case len(actions) > 0:
			fmt.Fprintf(&sb, "Start with the highest-severity item above (%s).", actions[0].Title)
		case len(r.Anomalies) > 0:
			fmt.Fprintf(&sb, "Investigate the %s anomaly before it compounds.", r.Anomalies[0].Metric)
		default:
			sb.WriteString("Metrics look steady — keep CI green and PR median age low.")
		}
	}

	_ = question
	return sb.String()
}

func explainWatchedPortfolio(question string, repos []insights.RepoInsights, actions []stats.ActionItem) string {
	sorted := slices.Clone(repos)
	slices.SortStableFunc(sorted, func(a, b insights.RepoInsights) int {
		if a.ContributorReadiness != b.ContributorReadiness {
			return b.ContributorReadiness - a.ContributorReadiness
		}
		return b.HealthScore - a.HealthScore
	})

	var sb strings.Builder
	sb.WriteString("**Watched Repositories — Contributor Evaluation Summary**\n\n")
	sb.WriteString("**Top picks by Contributor Readiness:**\n")
	for i, r := range sorted {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&sb, "%d. **%s** — Readiness **%d/100 (%s)** · Health %d/100 · %d commits/30d · PRs: %s (median %.1fd) · Diversity: %s (%.2f)\n",
			i+1, r.FullName, r.ContributorReadiness, r.ReadinessVerdict, r.HealthScore, r.CommitCount30d, r.Responsiveness, r.PRMedianAgeDays, r.DiversityLabel, r.DiversityScore)
	}

	var anomalies []string
	for _, r := range sorted {
		for _, a := range r.Anomalies {
			anomalies = append(anomalies, fmt.Sprintf("%s: %s", r.FullName, a.Summary))
		}
	}
	if len(anomalies) > 0 {
		sb.WriteString("\n**Notable Statistical Anomalies:**\n")
		for i, a := range anomalies {
			if i >= 4 {
				break
			}
			fmt.Fprintf(&sb, "- %s\n", a)
		}
	} else if strings.Contains(strings.ToLower(question), "anomal") {
		sb.WriteString("\nNo statistical Z-score anomalies (>= 1.5σ) detected across your watched repositories yet.\n")
	}

	if len(actions) > 0 {
		sb.WriteString("\n**Project Signals to Keep in Mind:**\n")
		for i, a := range actions {
			if i >= 3 {
				break
			}
			fmt.Fprintf(&sb, "- **%s:** %s\n", a.RepoName, a.Title)
		}
	}

	best := sorted[0]
	fmt.Fprintf(&sb, "\n**Next step:** Explore **%s** first (%s, %d commits in 30d).", best.FullName, best.ReadinessVerdict, best.CommitCount30d)
	return sb.String()
}

func explainMaintainerPortfolio(question string, repos []insights.RepoInsights, actions []stats.ActionItem) string {
	var sb strings.Builder
	var sumHealth, slipping int
	var anomalies []string
	for _, r := range repos {
		sumHealth += r.HealthScore
		if r.SampleCount >= 2 && r.HealthSlopePerDay < -0.1 {
			slipping++
		}
		for _, a := range r.Anomalies {
			anomalies = append(anomalies, fmt.Sprintf("**%s:** %s", r.FullName, a.Summary))
		}
	}
	avg := sumHealth / len(repos)
	fmt.Fprintf(&sb, "**My Repositories — Maintenance Briefing (%d repos, avg health %d/100)**\n\n", len(repos), avg)

	if slipping > 0 {
		fmt.Fprintf(&sb, "- **Trajectory Alert:** %d repo(s) currently show a negative health regression slope.\n", slipping)
	}
	if len(anomalies) > 0 {
		sb.WriteString("- **Statistical Anomalies (Z-score):**\n")
		for i, a := range anomalies {
			if i >= 4 {
				break
			}
			fmt.Fprintf(&sb, "  - %s\n", a)
		}
	} else if strings.Contains(strings.ToLower(question), "anomal") {
		sb.WriteString("- No Z-score anomalies (>= 1.5σ) detected across your repositories.\n")
	}

	if len(actions) > 0 {
		sb.WriteString("\n**Highest-Leverage Actions:**\n")
		for i, a := range actions {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&sb, "%d. **%s** — %s (Severity %d/5)\n", i+1, a.RepoName, a.Title, a.Severity)
		}
	} else {
		sb.WriteString("\nNo active maintenance alerts right now — your repositories are in good shape.")
	}
	return sb.String()
}

func collectLinks(repos []insights.RepoInsights, actions []stats.ActionItem) []Link {
	seen := map[string]bool{}
	var out []Link
	for _, a := range actions {
		if a.URL != "" && !seen[a.URL] {
			seen[a.URL] = true
			out = append(out, Link{Label: a.RepoName + ": " + a.Rule, URL: a.URL})
			if len(out) >= 5 {
				return out
			}
		}
	}
	for _, r := range repos {
		u := "https://github.com/" + r.FullName
		if !seen[u] {
			seen[u] = true
			out = append(out, Link{Label: r.FullName, URL: u})
			if len(out) >= 5 {
				break
			}
		}
	}
	if out == nil {
		out = []Link{}
	}
	return out
}
