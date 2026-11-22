package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PR assistant modes.
const (
	PRModeOff       = "off"
	PRModeDashboard = "dashboard" // reviewed and shown in gitpulse; nothing posted
	PRModeComment   = "comment"   // also posts/edits one comment on the PR
)

// RepoSetting is a repo's automation switches. Every write is off by default.
type RepoSetting struct {
	RepoID          int64  `json:"repoId"`
	FullName        string `json:"fullName"`
	Visibility      string `json:"visibility"`
	IsArchived      bool   `json:"isArchived"`
	IsFork          bool   `json:"isFork"`
	PRMode          string `json:"prMode"`
	ReleasesEnabled bool   `json:"releasesEnabled"`
}

func DefaultRepoSetting(repoID int64) RepoSetting {
	return RepoSetting{RepoID: repoID, PRMode: PRModeDashboard}
}

func ValidPRMode(m string) bool {
	return m == PRModeOff || m == PRModeDashboard || m == PRModeComment
}

const repoSettingsSelect = `
	SELECT r.id, r.full_name, coalesce(s.visibility,''), coalesce(s.is_archived,false), coalesce(s.is_fork,false),
	       coalesce(rs.pr_mode,'dashboard'), coalesce(rs.releases_enabled,false)
	FROM repos r
	LEFT JOIN repo_snapshots s ON s.repo_id = r.id AND s.scan_id = r.last_seen_scan_id
	LEFT JOIN repo_settings rs ON rs.repo_id = r.id AND rs.user_id = r.user_id
	WHERE r.user_id = $1`

func scanRepoSetting(row pgx.Row) (RepoSetting, error) {
	var s RepoSetting
	err := row.Scan(&s.RepoID, &s.FullName, &s.Visibility, &s.IsArchived, &s.IsFork, &s.PRMode, &s.ReleasesEnabled)
	return s, err
}

// ListRepoSettings returns every repo gitpulse knows for the user, with its
// effective settings (defaults when no row exists).
func (s *Store) ListRepoSettings(ctx context.Context, userID int64) ([]RepoSetting, error) {
	rows, err := s.DB.Query(ctx, repoSettingsSelect+` ORDER BY lower(r.full_name)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoSetting{}
	for rows.Next() {
		rs, err := scanRepoSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rs)
	}
	return out, rows.Err()
}

// RepoSettingFor returns one repo's effective settings. ErrNotFound if the
// repo is not the user's.
func (s *Store) RepoSettingFor(ctx context.Context, userID, repoID int64) (RepoSetting, error) {
	rs, err := scanRepoSetting(s.DB.QueryRow(ctx, repoSettingsSelect+` AND r.id = $2`, userID, repoID))
	if errors.Is(err, pgx.ErrNoRows) {
		return rs, ErrNotFound
	}
	return rs, err
}

// SaveRepoSetting stores the switches for a repo the user owns.
func (s *Store) SaveRepoSetting(ctx context.Context, userID int64, in RepoSetting) (RepoSetting, error) {
	if !ValidPRMode(in.PRMode) {
		return in, fmt.Errorf("%w: prMode must be off, dashboard or comment", ErrInvalid)
	}
	tag, err := s.DB.Exec(ctx, `
		INSERT INTO repo_settings (user_id, repo_id, pr_mode, releases_enabled)
		SELECT $1, id, $3, $4 FROM repos WHERE id = $2 AND user_id = $1
		ON CONFLICT (user_id, repo_id) DO UPDATE SET
		  pr_mode = EXCLUDED.pr_mode, releases_enabled = EXCLUDED.releases_enabled, updated_at = now()`,
		userID, in.RepoID, in.PRMode, in.ReleasesEnabled)
	if err != nil {
		return in, err
	}
	if tag.RowsAffected() == 0 {
		return in, ErrNotFound
	}
	return s.RepoSettingFor(ctx, userID, in.RepoID)
}

// ErrInvalid wraps input validation failures.
var ErrInvalid = errors.New("invalid input")
