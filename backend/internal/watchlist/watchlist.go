// Package watchlist manages external public repositories that a user tracks
// alongside their own repositories. Watched repositories are strictly read-only.
package watchlist

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/github"
)

const MaxWatchedRepos = 50

var (
	ErrInvalid        = errors.New("invalid repository; use owner/name or a GitHub URL")
	ErrLimitReached   = fmt.Errorf("watchlist limit reached (max %d repositories)", MaxWatchedRepos)
	ErrAlreadyWatched = errors.New("repository is already in your watchlist")
	ErrNotFound       = errors.New("watched repository not found")
	ErrPrivateRepo    = errors.New("private repositories cannot be watched")
)

var validSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// WatchedRepo is one row of watched_repos.
type WatchedRepo struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Owner     string    `json:"owner"`
	Name      string    `json:"name"`
	FullName  string    `json:"fullName"`
	GitHubID  *int64    `json:"githubId,omitempty"`
	AddedAt   time.Time `json:"addedAt"`
	LastError string    `json:"lastError,omitempty"`
}

// RepoMeta is the minimal metadata needed to validate a public repo on Add.
type RepoMeta struct {
	ID      int64
	Owner   string
	Name    string
	Private bool
}

// RepoLookup checks whether a public repository exists on GitHub.
type RepoLookup interface {
	LookupPublicRepo(ctx context.Context, userID int64, owner, name string) (*RepoMeta, error)
}

// ParseRepoInput extracts (owner, name) from "owner/name",
// "https://github.com/owner/name", or "https://github.com/owner/name.git".
func ParseRepoInput(raw string) (owner, name string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", ErrInvalid
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || !strings.EqualFold(u.Host, "github.com") {
			return "", "", ErrInvalid
		}
		s = strings.Trim(u.Path, "/")
	} else if strings.HasPrefix(strings.ToLower(s), "github.com/") {
		s = s[len("github.com/"):]
	}
	s = strings.TrimSuffix(strings.Trim(s, "/"), ".git")
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", "", ErrInvalid
	}
	owner, name = parts[0], parts[1]
	if !validSlug.MatchString(owner) || !validSlug.MatchString(name) || name == "." || name == ".." {
		return "", "", ErrInvalid
	}
	return owner, name, nil
}

type Service struct {
	db     *pgxpool.Pool
	lookup RepoLookup
}

func New(db *pgxpool.Pool, lookup RepoLookup) *Service {
	return &Service{db: db, lookup: lookup}
}

func (s *Service) List(ctx context.Context, userID int64) ([]WatchedRepo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, owner, name, github_id, added_at, coalesce(last_error, '')
		FROM watched_repos
		WHERE user_id = $1
		ORDER BY added_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (WatchedRepo, error) {
		var w WatchedRepo
		err := r.Scan(&w.ID, &w.UserID, &w.Owner, &w.Name, &w.GitHubID, &w.AddedAt, &w.LastError)
		w.FullName = w.Owner + "/" + w.Name
		return w, err
	})
}

func (s *Service) Add(ctx context.Context, userID int64, raw string) (*WatchedRepo, error) {
	owner, name, err := ParseRepoInput(raw)
	if err != nil {
		return nil, err
	}

	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM watched_repos WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxWatchedRepos {
		return nil, ErrLimitReached
	}

	var ghID *int64
	if s.lookup != nil {
		meta, err := s.lookup.LookupPublicRepo(ctx, userID, owner, name)
		if err != nil {
			if errors.Is(err, github.ErrPrivateRepo) {
				return nil, ErrPrivateRepo
			}
			if github.IsNotFound(err) {
				return nil, fmt.Errorf("%w: repository %s/%s not found on GitHub", ErrNotFound, owner, name)
			}
			return nil, err
		}
		if meta.Private {
			return nil, ErrPrivateRepo
		}
		if meta.Owner != "" {
			owner = meta.Owner
		}
		if meta.Name != "" {
			name = meta.Name
		}
		if meta.ID != 0 {
			id := meta.ID
			ghID = &id
		}
	}

	var w WatchedRepo
	err = s.db.QueryRow(ctx, `
		INSERT INTO watched_repos (user_id, owner, name, github_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, owner, name, github_id, added_at, coalesce(last_error, '')`,
		userID, owner, name, ghID).Scan(&w.ID, &w.UserID, &w.Owner, &w.Name, &w.GitHubID, &w.AddedAt, &w.LastError)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyWatched
		}
		return nil, err
	}
	w.FullName = w.Owner + "/" + w.Name
	return &w, nil
}

func (s *Service) Remove(ctx context.Context, userID, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM watched_repos WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) SetError(ctx context.Context, id int64, errMsg string) error {
	_, err := s.db.Exec(ctx, `UPDATE watched_repos SET last_error = NULLIF($2, '') WHERE id = $1`, id, errMsg)
	return err
}

func (s *Service) MarkScanned(ctx context.Context, id, githubID int64) error {
	_, err := s.db.Exec(ctx, `UPDATE watched_repos SET github_id = $2, last_error = NULL WHERE id = $1`, id, githubID)
	return err
}
