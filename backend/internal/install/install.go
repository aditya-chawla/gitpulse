// Package install connects gitpulse users to their GitHub App installation
// and hands out API clients authenticated with installation tokens.
package install

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

var ErrSuspended = errors.New("github app installation is suspended")

// MissingPermissionsError means the installation hasn't been granted (or the
// account hasn't accepted) permissions gitpulse needs.
type MissingPermissionsError struct{ Missing []string }

func (e *MissingPermissionsError) Error() string {
	return "github app installation lacks permissions " + strings.Join(e.Missing, ", ") +
		": add them in the app's Permissions settings, then accept the change on the installation page"
}

type Service struct {
	app *github.App
	db  *pgxpool.Pool
	log *slog.Logger
}

func New(app *github.App, db *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{app: app, db: db, log: log}
}

func (s *Service) App() *github.App { return s.app }

// Lookup asks GitHub for the user's installation and records it. If GitHub is
// unreachable, the last recorded installation is used instead.
func (s *Service) Lookup(ctx context.Context, userID int64) (*github.Installation, error) {
	in, err := s.app.InstallationFor(ctx, userID)
	switch {
	case errors.Is(err, github.ErrNotInstalled):
		if _, derr := s.db.Exec(ctx, `DELETE FROM github_installations WHERE account_id = $1`, userID); derr != nil {
			s.log.Error("forget installation", "err", derr)
		}
		return nil, err
	case err != nil:
		if cached, cerr := s.Cached(ctx, userID); cerr == nil {
			s.log.Warn("installation lookup failed; using recorded installation", "err", err)
			return cached, nil
		}
		return nil, err
	}
	if err := s.Save(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

// Save records an installation (also called from webhooks).
func (s *Service) Save(ctx context.Context, in *github.Installation) error {
	perms := in.Permissions
	if perms == nil {
		perms = map[string]string{}
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO github_installations (id, account_id, account_login, repository_selection, html_url, permissions, suspended_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (account_id) DO UPDATE SET
		  id = EXCLUDED.id, account_login = EXCLUDED.account_login,
		  repository_selection = EXCLUDED.repository_selection, html_url = EXCLUDED.html_url,
		  permissions = EXCLUDED.permissions, suspended_at = EXCLUDED.suspended_at, updated_at = now()`,
		in.ID, in.AccountID, in.AccountLogin, in.RepositorySelection, in.HTMLURL, perms, in.SuspendedAt)
	if err != nil {
		return fmt.Errorf("save installation: %w", err)
	}
	return nil
}

// Delete forgets an installation (uninstall webhook).
func (s *Service) Delete(ctx context.Context, installationID int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM github_installations WHERE id = $1`, installationID)
	return err
}

// Cached returns the recorded installation without calling GitHub.
func (s *Service) Cached(ctx context.Context, userID int64) (*github.Installation, error) {
	var in github.Installation
	err := s.db.QueryRow(ctx, `
		SELECT id, account_id, account_login, repository_selection, coalesce(html_url,''), permissions, suspended_at
		FROM github_installations WHERE account_id = $1`, userID).
		Scan(&in.ID, &in.AccountID, &in.AccountLogin, &in.RepositorySelection, &in.HTMLURL, &in.Permissions, &in.SuspendedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, github.ErrNotInstalled
	}
	return &in, err
}

func (s *Service) active(ctx context.Context, userID int64) (*github.Installation, error) {
	in, err := s.Lookup(ctx, userID)
	if err != nil {
		return nil, err
	}
	if in.SuspendedAt != nil {
		return nil, ErrSuspended
	}
	return in, nil
}

// ReadClient returns a client limited to read permissions on every repo the
// installation covers. Scans use this.
func (s *Service) ReadClient(ctx context.Context, u *store.User, opts ...github.Option) (*github.HTTPClient, error) {
	in, err := s.active(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if missing := in.MissingFor(github.ReadOnly); len(missing) > 0 {
		return nil, &MissingPermissionsError{Missing: missing}
	}
	return github.NewWithTokenFunc(s.app.TokenFunc(in.ID, github.ReadOnly), opts...), nil
}

// RepoToken mints a token scoped to a single repository and exactly the given
// permissions. Write features use this so a token can never touch other repos.
func (s *Service) RepoToken(ctx context.Context, u *store.User, repoID int64, perms map[string]string) (string, error) {
	in, err := s.active(ctx, u.ID)
	if err != nil {
		return "", err
	}
	if missing := in.MissingFor(github.TokenRequest{Permissions: perms}); len(missing) > 0 {
		return "", &MissingPermissionsError{Missing: missing}
	}
	return s.app.InstallationToken(ctx, in.ID, github.TokenRequest{RepositoryIDs: []int64{repoID}, Permissions: perms})
}

// BotLogin is the app's bot account, which authors its comments.
func (s *Service) BotLogin(ctx context.Context) (string, error) {
	info, err := s.app.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Slug + "[bot]", nil
}
