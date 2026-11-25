// Package advisor powers the context-aware "Ask GitPulse" assistant.
// It grounds answers in Postgres-computed Data Science metrics (insights) and
// action items, supporting zero-cost deterministic explanations as well as
// user-supplied Bring-Your-Own-Key (BYOK) calls to Gemini, OpenAI, and Claude.
package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
)

const (
	RoleMaintainer  = "maintainer"
	RoleContributor = "contributor"
)

type StatsReader interface {
	Insights(ctx context.Context, userID int64, source string) ([]insights.RepoInsights, error)
	RepoInsights(ctx context.Context, userID, repoID int64) (*insights.RepoInsights, error)
	ActionItemsForSource(ctx context.Context, userID int64, limit int, source string) ([]stats.ActionItem, error)
}

type AskRequest struct {
	Role     string `json:"role"`
	RepoID   int64  `json:"repoId"`
	Question string `json:"question"`
	Provider string `json:"provider"` // "" | "deterministic" | "gemini" | "openai" | "anthropic"
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type AskResponse struct {
	Answer  string `json:"answer"`
	Mode    string `json:"mode"`
	Model   string `json:"model,omitempty"`
	Role    string `json:"role"`
	Links   []Link `json:"links"`
	Warning string `json:"warning,omitempty"`
}

type Service struct {
	stats StatsReader
	http  *http.Client
	// Overridable endpoints for tests.
	geminiBaseURL    string
	openAIURL        string
	anthropicBaseURL string
}

func New(st StatsReader) *Service {
	return &Service{
		stats:            st,
		http:             &http.Client{Timeout: 25 * time.Second},
		geminiBaseURL:    "https://generativelanguage.googleapis.com/v1beta/models",
		openAIURL:        "https://api.openai.com/v1/chat/completions",
		anthropicBaseURL: "https://api.anthropic.com/v1/messages",
	}
}

func (s *Service) Ask(ctx context.Context, userID int64, req AskRequest) (*AskResponse, error) {
	role := normalizeRole(req.Role)
	source := sourceForRole(role)
	question := strings.TrimSpace(req.Question)
	if question == "" {
		if role == RoleContributor {
			question = "Which watched repository is best to contribute to right now, and why?"
		} else {
			question = "What should I prioritize fixing across my repositories?"
		}
	}

	var (
		repos   []insights.RepoInsights
		actions []stats.ActionItem
		err     error
	)

	if req.RepoID > 0 {
		ri, rerr := s.stats.RepoInsights(ctx, userID, req.RepoID)
		if rerr != nil {
			return nil, rerr
		}
		repos = []insights.RepoInsights{*ri}
		if ri.Source == "watched" {
			role = RoleContributor
			source = "watched"
		} else {
			role = RoleMaintainer
			source = "owned"
		}
		allActs, aerr := s.stats.ActionItemsForSource(ctx, userID, 20, source)
		if aerr == nil {
			for _, a := range allActs {
				if a.RepoID == req.RepoID {
					actions = append(actions, a)
				}
			}
		}
	} else {
		repos, err = s.stats.Insights(ctx, userID, source)
		if err != nil {
			if errors.Is(err, stats.ErrNoData) {
				return &AskResponse{
					Answer: "No completed scan data is available yet. Run a scan first so GitPulse can compute health scores, trends, and Data Science signals.",
					Mode:   "deterministic",
					Role:   role,
					Links:  []Link{},
				}, nil
			}
			return nil, err
		}
		actions, _ = s.stats.ActionItemsForSource(ctx, userID, 10, source)
	}

	det := buildDeterministic(role, question, repos, actions)

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	apiKey := strings.TrimSpace(req.APIKey)
	if provider == "" || provider == "deterministic" || apiKey == "" {
		return det, nil
	}

	sysPrompt := systemPrompt(role)
	userPrompt := buildLLMPrompt(role, question, repos, actions)
	answer, modelUsed, callErr := s.callBYOK(ctx, provider, req.Model, apiKey, sysPrompt, userPrompt)
	if callErr != nil {
		det.Warning = fmt.Sprintf("BYOK %s call failed (%s); showing deterministic analysis instead.", provider, callErr.Error())
		return det, nil
	}

	return &AskResponse{
		Answer: answer,
		Mode:   provider,
		Model:  modelUsed,
		Role:   role,
		Links:  det.Links,
	}, nil
}

func normalizeRole(r string) string {
	if strings.EqualFold(strings.TrimSpace(r), RoleContributor) {
		return RoleContributor
	}
	return RoleMaintainer
}

func sourceForRole(role string) string {
	if role == RoleContributor {
		return "watched"
	}
	return "owned"
}

func systemPrompt(role string) string {
	if role == RoleContributor {
		return "You are GitPulse Contributor Advisor, helping an open-source developer evaluate external public repositories. " +
			"Explain the Postgres Data Science metrics (contributorReadiness, diversityScore, responsiveness, prMedianAgeDays, healthSlopePerDay, projectedHealth30d, and Z-score anomalies) clearly and concisely. " +
			"Never assume the user owns or maintains these repos. Recommend whether to contribute, what to expect for PR review times, and point to specific repos or issues. Keep responses concise and structured in Markdown."
	}
	return "You are GitPulse Maintainer Copilot, helping a repository owner prioritize maintenance across their own GitHub repositories. " +
		"Explain the Postgres Data Science metrics (healthScore, healthSlopePerDay, projectedHealth30d, prMedianAgeDays, prStalePct, diversityScore, and Z-score anomalies) clearly and concisely. " +
		"Point directly to the highest-impact fixes, failing CI workflows, or aging PRs. Keep responses concise and structured in Markdown."
}

func buildLLMPrompt(role, question string, repos []insights.RepoInsights, actions []stats.ActionItem) string {
	if len(repos) > 15 {
		repos = repos[:15]
	}
	if len(actions) > 10 {
		actions = actions[:10]
	}
	payload := struct {
		Role    string                  `json:"role"`
		Repos   []insights.RepoInsights `json:"repos"`
		Signals []stats.ActionItem      `json:"signals"`
	}{
		Role:    role,
		Repos:   repos,
		Signals: actions,
	}
	b, _ := json.Marshal(payload)
	return fmt.Sprintf("User question: %s\n\nSnapshot Data Science metrics & signals (JSON):\n%s", question, string(b))
}
