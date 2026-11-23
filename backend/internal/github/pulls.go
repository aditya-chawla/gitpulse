package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PullRequest is the subset of a PR the PR assistant needs.
type PullRequest struct {
	Number       int
	Title        string
	Body         string
	Author       string
	AuthorIsBot  bool
	State        string // open | closed
	Draft        bool
	HeadSHA      string
	BaseRef      string
	URL          string
	Additions    int
	Deletions    int
	ChangedFiles int
	Labels       []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PRFile is one changed file. Patch is empty for binary files and for diffs
// GitHub considers too large to show.
type PRFile struct {
	Path         string
	PreviousPath string
	Status       string // added | removed | modified | renamed | copied | changed | unchanged
	Additions    int
	Deletions    int
	Changes      int
	Patch        string
}

type IssueComment struct {
	ID      int64
	Body    string
	Author  string
	HTMLURL string
}

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	User   struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	HTMLURL      string `json:"html_url"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changed_files"`
	Labels       []struct {
		Name string `json:"name"`
	} `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (g ghPR) toPR() PullRequest {
	p := PullRequest{
		Number: g.Number, Title: g.Title, Body: g.Body, Author: g.User.Login, AuthorIsBot: g.User.Type == "Bot",
		State: g.State, Draft: g.Draft, HeadSHA: g.Head.SHA, BaseRef: g.Base.Ref, URL: g.HTMLURL,
		Additions: g.Additions, Deletions: g.Deletions, ChangedFiles: g.ChangedFiles,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
	for _, l := range g.Labels {
		p.Labels = append(p.Labels, l.Name)
	}
	return p
}

func (c *HTTPClient) getJSON(ctx context.Context, path string, out any) error {
	_, body, err := c.restGet(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// paginate calls fn for each page (100 per page) until a short page or max.
func (c *HTTPClient) paginate(ctx context.Context, path string, maxPages int, fn func([]byte) (n int, err error)) error {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 1; page <= maxPages; page++ {
		_, body, err := c.restGet(ctx, path+sep+"per_page=100&page="+strconv.Itoa(page))
		if err != nil {
			return err
		}
		n, err := fn(body)
		if err != nil {
			return err
		}
		if n < 100 {
			return nil
		}
	}
	return nil
}

// send performs a non-GET request with a JSON body.
func (c *HTTPClient) send(ctx context.Context, method, path string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	url := c.apiBase + path
	status, _, body, err := c.do(ctx, method, url, b, nil, false)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return &APIError{Status: status, URL: url, Body: string(body)}
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (c *HTTPClient) PullRequest(ctx context.Context, repo RepoRef, number int) (*PullRequest, error) {
	var g ghPR
	if err := c.getJSON(ctx, fmt.Sprintf("%s/pulls/%d", repoPath(repo), number), &g); err != nil {
		return nil, err
	}
	pr := g.toPR()
	return &pr, nil
}

// OpenPullRequests lists a repo's open PRs (newest first, up to 300).
func (c *HTTPClient) OpenPullRequests(ctx context.Context, repo RepoRef) ([]PullRequest, error) {
	var out []PullRequest
	err := c.paginate(ctx, repoPath(repo)+"/pulls?state=open&sort=created&direction=desc", 3, func(b []byte) (int, error) {
		var page []ghPR
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, g := range page {
			out = append(out, g.toPR())
		}
		return len(page), nil
	})
	return out, err
}

// PullRequestFiles lists changed files with patches (GitHub lists at most
// 3000 files; compare the count with PullRequest.ChangedFiles).
func (c *HTTPClient) PullRequestFiles(ctx context.Context, repo RepoRef, number int) ([]PRFile, error) {
	var out []PRFile
	err := c.paginate(ctx, fmt.Sprintf("%s/pulls/%d/files", repoPath(repo), number), 30, func(b []byte) (int, error) {
		var page []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Status           string `json:"status"`
			Additions        int    `json:"additions"`
			Deletions        int    `json:"deletions"`
			Changes          int    `json:"changes"`
			Patch            string `json:"patch"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, f := range page {
			out = append(out, PRFile{Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status,
				Additions: f.Additions, Deletions: f.Deletions, Changes: f.Changes, Patch: f.Patch})
		}
		return len(page), nil
	})
	return out, err
}

// PullRequestCommitMessages returns commit messages (first 250 commits).
func (c *HTTPClient) PullRequestCommitMessages(ctx context.Context, repo RepoRef, number int) ([]string, error) {
	var out []string
	err := c.paginate(ctx, fmt.Sprintf("%s/pulls/%d/commits", repoPath(repo), number), 3, func(b []byte) (int, error) {
		var page []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, p := range page {
			out = append(out, p.Commit.Message)
		}
		return len(page), nil
	})
	return out, err
}

// Labels lists the names of a repo's labels.
func (c *HTTPClient) Labels(ctx context.Context, repo RepoRef) ([]string, error) {
	var out []string
	err := c.paginate(ctx, repoPath(repo)+"/labels", 5, func(b []byte) (int, error) {
		var page []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, l := range page {
			out = append(out, l.Name)
		}
		return len(page), nil
	})
	return out, err
}

type ghComment struct {
	ID      int64  `json:"id"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (g ghComment) toComment() IssueComment {
	return IssueComment{ID: g.ID, Body: g.Body, Author: g.User.Login, HTMLURL: g.HTMLURL}
}

// IssueComments lists comments on an issue or PR (first 1000).
func (c *HTTPClient) IssueComments(ctx context.Context, repo RepoRef, number int) ([]IssueComment, error) {
	var out []IssueComment
	err := c.paginate(ctx, fmt.Sprintf("%s/issues/%d/comments", repoPath(repo), number), 10, func(b []byte) (int, error) {
		var page []ghComment
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, g := range page {
			out = append(out, g.toComment())
		}
		return len(page), nil
	})
	return out, err
}

func (c *HTTPClient) CreateIssueComment(ctx context.Context, repo RepoRef, number int, body string) (*IssueComment, error) {
	var g ghComment
	if err := c.send(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", repoPath(repo), number), map[string]string{"body": body}, &g); err != nil {
		return nil, err
	}
	ic := g.toComment()
	return &ic, nil
}

func (c *HTTPClient) UpdateIssueComment(ctx context.Context, repo RepoRef, commentID int64, body string) (*IssueComment, error) {
	var g ghComment
	if err := c.send(ctx, http.MethodPatch, fmt.Sprintf("%s/issues/comments/%d", repoPath(repo), commentID), map[string]string{"body": body}, &g); err != nil {
		return nil, err
	}
	ic := g.toComment()
	return &ic, nil
}
