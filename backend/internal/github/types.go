// Package github is gitpulse's GitHub API client. Everything the rest of the
// app needs from GitHub goes through the Client interface so it can be faked.
package github

import (
	"context"
	"time"
)

type Client interface {
	// User resolves a user by node ID (stable across renames).
	User(ctx context.Context, nodeID string) (*Viewer, error)
	// OwnedRepos lists every repo the user owns that the token can see, with
	// metadata, languages, topics, open PRs/issues, branches, and recent
	// forks. (Stargazer lists are not readable with installation tokens, so
	// only star counts are collected.)
	OwnedRepos(ctx context.Context, login string) ([]Repo, error)
	// Trees returns each repo's file tree at HEAD down to `depth` levels.
	// Empty repos are absent from the map.
	Trees(ctx context.Context, repos []RepoRef, depth int) (map[RepoRef][]TreeEntry, error)
	// FileContents fetches text blobs at HEAD. Missing files are absent.
	FileContents(ctx context.Context, files []FileRef) (map[FileRef]string, error)
	// CommitActivity returns 52 weeks of all-author commit counts. ok=false
	// means GitHub is still computing the stats.
	CommitActivity(ctx context.Context, repo RepoRef) (weeks []CommitWeek, ok bool, err error)
	// CommitsBy lists default-branch commits authored by authorNodeID since t.
	CommitsBy(ctx context.Context, repo RepoRef, authorNodeID string, since time.Time) ([]Commit, error)
	// FailedRuns lists failed Actions workflow runs created since t.
	FailedRuns(ctx context.Context, repo RepoRef, since time.Time) ([]WorkflowRun, error)
	Rate() RateInfo
}

type RepoRef struct{ Owner, Name string }

func (r RepoRef) String() string { return r.Owner + "/" + r.Name }

type FileRef struct {
	Repo RepoRef
	Path string
}

type Viewer struct {
	ID     int64
	NodeID string
	Login  string
}

type Repo struct {
	ID            int64
	NodeID        string
	Ref           RepoRef
	Description   string
	Visibility    string // public | private | internal
	IsFork        bool
	IsArchived    bool
	IsEmpty       bool
	Stars         int
	Forks         int
	SizeKB        int
	DefaultBranch string
	Topics        []string
	LicenseSPDX   string
	CreatedAt     time.Time
	PushedAt      *time.Time

	Languages []LanguageBytes

	OpenPRCount    int
	OpenIssueCount int
	OpenPRs        []Item // first 50
	OpenIssues     []Item // first 50

	Branches          []Branch
	BranchesTruncated bool

	RecentForks []Actor
}

type LanguageBytes struct {
	Language string
	Bytes    int64
}

type Item struct {
	Number         int
	Title          string
	Author         string
	URL            string
	IsDraft        bool
	ReviewRequests []string // logins
	Assignees      []string // logins
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Branch struct {
	Name         string
	LastCommitAt *time.Time
}

type Actor struct {
	Login string
	At    time.Time
}

type TreeEntry struct {
	Path  string // slash-separated, relative to repo root
	IsDir bool
}

type CommitWeek struct {
	WeekStart time.Time // Sunday, UTC
	Total     int
	Days      [7]int // Sunday..Saturday
}

type Commit struct {
	SHA        string
	AuthoredAt time.Time
	Additions  int
	Deletions  int
}

type WorkflowRun struct {
	ID         int64
	Name       string
	Branch     string
	Conclusion string
	URL        string
	CreatedAt  time.Time
}

type RateInfo struct {
	RESTRemaining    int
	GraphQLRemaining int
	Calls            int
}

// Cache stores ETag'd REST responses for conditional requests.
type Cache interface {
	Get(ctx context.Context, key string) (etag string, body []byte, ok bool)
	Put(ctx context.Context, key, etag string, body []byte)
}
