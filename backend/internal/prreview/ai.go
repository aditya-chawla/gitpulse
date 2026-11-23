package prreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
)

// AIReview is the optional model-written part. Every field is sanitised
// before it is stored; labels are filtered against the repo's real labels.
type AIReview struct {
	Summary         string   `json:"summary"`
	Notable         []string `json:"notable"`
	SuggestedTitle  string   `json:"suggestedTitle"`
	SuggestedLabels []string `json:"suggestedLabels"`
}

const promptVersion = "pr-review-v1"

const systemPrompt = `You review GitHub pull requests for the repository owner. From the material provided, write:
- summary: 2 to 4 plain sentences on what the change does and why, for someone deciding whether to review it now.
- notable: up to 6 short bullet points (one sentence each) on risky or notable changes: database migrations, auth or security code, configuration and deployment, dependency changes, deleted or weakened tests, large or generated files, behaviour changes that are easy to miss. Empty if nothing stands out.
- suggestedTitle: a clear title under 72 characters; use a conventional-commit prefix (feat:, fix:, docs:, chore:, refactor:, test:, ci:, perf:, build:) when the change fits one. Repeat the current title if it is already good.
- suggestedLabels: zero to three labels, chosen only from the AVAILABLE LABELS list, exactly as written there.
Be factual and concise; do not speculate beyond the diff. Parts of the diff may be truncated or omitted, as stated.
` + llm.UntrustedRules

var aiSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":         map[string]any{"type": "string"},
		"notable":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"suggestedTitle":  map[string]any{"type": "string"},
		"suggestedLabels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required":             []string{"summary", "notable", "suggestedTitle", "suggestedLabels"},
	"additionalProperties": false,
}

// buildPrompt assembles the user message. Repo labels come from the repo
// owner but are still listed as plain data.
func buildPrompt(pr github.PullRequest, commits []string, repoLabels []string, a Analysis, diff string) string {
	var b strings.Builder
	labels, _ := json.Marshal(repoLabels)
	fmt.Fprintf(&b, "AVAILABLE LABELS: %s\n\n", labels)
	fmt.Fprintf(&b, "Rule-based findings (trusted): %d files changed, +%d -%d.", a.FilesChanged, a.Additions, a.Deletions)
	for _, f := range a.Flags {
		fmt.Fprintf(&b, " %s: %s.", f.Title, strings.Join(f.Files, ", "))
	}
	if len(a.Omitted) > 0 {
		fmt.Fprintf(&b, " %d files omitted or truncated in the diff below.", len(a.Omitted))
	}
	b.WriteString("\n\n")
	body := pr.Body
	if len(body) > 4000 {
		body = body[:4000] + "\n[... description truncated]"
	}
	b.WriteString(llm.WrapUntrusted("pull request title", pr.Title) + "\n")
	b.WriteString(llm.WrapUntrusted("pull request description", body) + "\n")
	if len(commits) > 0 {
		cm := commits
		if len(cm) > 50 {
			cm = cm[:50]
		}
		var msgs strings.Builder
		for _, m := range cm {
			first, _, _ := strings.Cut(m, "\n")
			msgs.WriteString("- " + first + "\n")
		}
		b.WriteString(llm.WrapUntrusted("commit messages", msgs.String()) + "\n")
	}
	b.WriteString(llm.WrapUntrusted("diff", diff))
	return b.String()
}

// aiStatus maps an llm error to the stored status.
func aiStatus(err error, meta llm.Meta) string {
	switch {
	case err == nil && meta.Cached:
		return "cached"
	case err == nil:
		return "ok"
	case errors.Is(err, llm.ErrDisabled):
		return "disabled"
	case errors.Is(err, llm.ErrOverBudget):
		return "over_budget"
	case errors.Is(err, llm.ErrTooLarge):
		return "too_large"
	case errors.Is(err, llm.ErrRefused):
		return "refused"
	default:
		return "error"
	}
}

// summarize asks the model for the AI part; nil means "use rule-based only".
func summarize(ctx context.Context, ai *llm.Service, userID, repoID int64, pr github.PullRequest, commits, repoLabels []string, a Analysis, diff string) (*AIReview, string) {
	var out AIReview
	meta, err := ai.Run(ctx, llm.Task{
		Feature: "pr_review", UserID: userID, RepoID: repoID, PromptVersion: promptVersion,
		System: systemPrompt, Input: buildPrompt(pr, commits, repoLabels, a, diff), Schema: aiSchema,
	}, &out)
	status := aiStatus(err, meta)
	if err != nil {
		return nil, status
	}
	clean := &AIReview{
		Summary:         llm.SanitizeText(out.Summary, 1200),
		SuggestedTitle:  llm.SanitizeLine(out.SuggestedTitle, 100),
		SuggestedLabels: FilterLabels(out.SuggestedLabels, repoLabels),
	}
	for i, n := range out.Notable {
		if i == 6 {
			break
		}
		if s := llm.SanitizeLine(n, 300); s != "" {
			clean.Notable = append(clean.Notable, s)
		}
	}
	if len(clean.SuggestedLabels) > 3 {
		clean.SuggestedLabels = clean.SuggestedLabels[:3]
	}
	return clean, status
}

// aiNote is the comment footer prefix for non-AI outcomes.
func aiNote(status string) string {
	switch status {
	// "disabled": nothing to say on a public PR; the dashboard shows the note.
	case "over_budget":
		return "AI summary skipped: monthly budget reached."
	case "too_large":
		return "AI summary skipped: diff too large."
	case "refused", "error":
		return "AI summary unavailable for this commit."
	}
	return ""
}
