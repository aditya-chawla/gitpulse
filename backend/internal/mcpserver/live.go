package mcpserver

import (
	"context"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/prreview"
	"github.com/harshit3011/gitpulse/backend/internal/releases"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// Live serves the tools from the real services. MCP_TOKEN identifies one
// gitpulse user (Login), who must have signed in once and be allowlisted.
type Live struct {
	Store    *store.Store
	Stats    *stats.Service
	PRs      *prreview.Service
	Releases *releases.Service
	Login    string
	Allowed  func(login string) bool
}

func (l Live) User(ctx context.Context) (*store.User, error) {
	u, err := l.Store.GetUserByLogin(ctx, l.Login)
	if err != nil {
		return nil, err
	}
	if !l.Allowed(u.Login) {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func (l Live) Repos(ctx context.Context, u *store.User) (*stats.ReposResponse, error) {
	return l.Stats.Repos(ctx, u.ID)
}

func (l Live) Repo(ctx context.Context, u *store.User, id int64) (*stats.RepoDetail, error) {
	return l.Stats.Repo(ctx, u.ID, id)
}

func (l Live) Analytics(ctx context.Context, u *store.User) (*stats.Analytics, error) {
	return l.Stats.Analytics(ctx, u.ID)
}

func (l Live) Digest(ctx context.Context, u *store.User) (*stats.Digest, error) {
	return l.Stats.Digest(ctx, u.ID, u.Login)
}

func (l Live) PRReviews(ctx context.Context, u *store.User, limit int) ([]prreview.ReviewRow, error) {
	return l.PRs.Recent(ctx, u.ID, limit)
}

func (l Live) ReleasePreview(ctx context.Context, u *store.User, id int64) (*releases.Preview, error) {
	return l.Releases.Preview(ctx, u, id)
}

func (l Live) CreateRelease(ctx context.Context, u *store.User, id int64, in releases.CreateInput) (*github.Release, error) {
	return l.Releases.Create(ctx, u, id, in, "mcp")
}

func (l Live) TrendsOverview(ctx context.Context, u *store.User) ([]stats.RepoTrend, error) {
	return l.Stats.TrendsOverview(ctx, u.ID)
}

func (l Live) TrendForRepo(ctx context.Context, u *store.User, repoID int64) (*stats.RepoTrendDetail, error) {
	return l.Stats.TrendForRepo(ctx, u.ID, repoID)
}

func (l Live) ActionItems(ctx context.Context, u *store.User, limit int) ([]stats.ActionItem, error) {
	return l.Stats.ActionItems(ctx, u.ID, limit)
}

func (l Live) DismissActionItem(ctx context.Context, u *store.User, id int64) error {
	return l.Stats.DismissActionItem(ctx, u.ID, id)
}

func (l Live) Insights(ctx context.Context, u *store.User, source string) ([]insights.RepoInsights, error) {
	return l.Stats.Insights(ctx, u.ID, source)
}

func (l Live) RepoInsights(ctx context.Context, u *store.User, repoID int64) (*insights.RepoInsights, error) {
	return l.Stats.RepoInsights(ctx, u.ID, repoID)
}
