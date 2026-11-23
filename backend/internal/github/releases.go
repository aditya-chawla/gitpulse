package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Release struct {
	ID         int64
	TagName    string
	Name       string
	Draft      bool
	Prerelease bool
	HTMLURL    string
	CreatedAt  time.Time
}

type Tag struct {
	Name string
	SHA  string
}

// MergedPR is a merged pull request found by search.
type MergedPR struct {
	Number   int
	Title    string
	Body     string
	URL      string
	Author   string
	IsBot    bool
	Labels   []string
	MergedAt time.Time
}

type RepoInfo struct {
	ID            int64
	FullName      string
	DefaultBranch string
}

func (c *HTTPClient) Repo(ctx context.Context, repo RepoRef) (*RepoInfo, error) {
	var g struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getJSON(ctx, repoPath(repo), &g); err != nil {
		return nil, err
	}
	return &RepoInfo{ID: g.ID, FullName: g.FullName, DefaultBranch: g.DefaultBranch}, nil
}

// LatestRelease returns the newest published, non-prerelease release, or nil.
func (c *HTTPClient) LatestRelease(ctx context.Context, repo RepoRef) (*Release, error) {
	var g struct {
		ID         int64     `json:"id"`
		TagName    string    `json:"tag_name"`
		Name       string    `json:"name"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		HTMLURL    string    `json:"html_url"`
		CreatedAt  time.Time `json:"created_at"`
	}
	if err := c.getJSON(ctx, repoPath(repo)+"/releases/latest", &g); err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &Release{ID: g.ID, TagName: g.TagName, Name: g.Name, Draft: g.Draft, Prerelease: g.Prerelease, HTMLURL: g.HTMLURL, CreatedAt: g.CreatedAt}, nil
}

// Tags lists up to 300 tags (GitHub returns them in reverse name order).
func (c *HTTPClient) Tags(ctx context.Context, repo RepoRef) ([]Tag, error) {
	var out []Tag
	err := c.paginate(ctx, repoPath(repo)+"/tags", 3, func(b []byte) (int, error) {
		var page []struct {
			Name   string `json:"name"`
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return 0, err
		}
		for _, t := range page {
			out = append(out, Tag{Name: t.Name, SHA: t.Commit.SHA})
		}
		return len(page), nil
	})
	if IsNotFound(err) {
		return nil, nil
	}
	return out, err
}

// CommitDate returns a commit's committer date.
func (c *HTTPClient) CommitDate(ctx context.Context, repo RepoRef, sha string) (time.Time, error) {
	var g struct {
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, repoPath(repo)+"/commits/"+url.PathEscape(sha), &g); err != nil {
		return time.Time{}, err
	}
	return g.Commit.Committer.Date, nil
}

// MergedPRs finds PRs merged into base after since (zero = all time), newest
// first, up to max. Uses the search API (30 requests/minute).
func (c *HTTPClient) MergedPRs(ctx context.Context, repo RepoRef, base string, since time.Time, max int) ([]MergedPR, error) {
	q := fmt.Sprintf("repo:%s is:pr is:merged base:%s", repo, base)
	if !since.IsZero() {
		q += " merged:>" + since.UTC().Format(time.RFC3339)
	}
	var out []MergedPR
	for page := 1; page <= 10 && len(out) < max; page++ {
		path := "/search/issues?" + url.Values{"q": {q}, "sort": {"created"}, "order": {"desc"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}.Encode()
		var res struct {
			Items []struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				Body    string `json:"body"`
				HTMLURL string `json:"html_url"`
				User    struct {
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"user"`
				Labels []struct {
					Name string `json:"name"`
				} `json:"labels"`
				PullRequest struct {
					MergedAt *time.Time `json:"merged_at"`
				} `json:"pull_request"`
			} `json:"items"`
		}
		if err := c.getJSON(ctx, path, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			if it.PullRequest.MergedAt == nil || (!since.IsZero() && !it.PullRequest.MergedAt.After(since)) {
				continue
			}
			m := MergedPR{Number: it.Number, Title: it.Title, Body: it.Body, URL: it.HTMLURL, Author: it.User.Login,
				IsBot: it.User.Type == "Bot", MergedAt: *it.PullRequest.MergedAt}
			for _, l := range it.Labels {
				m.Labels = append(m.Labels, l.Name)
			}
			out = append(out, m)
		}
		if len(res.Items) < 100 {
			break
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return out, nil
}

// CreateDraftRelease always creates a draft: gitpulse never publishes.
func (c *HTTPClient) CreateDraftRelease(ctx context.Context, repo RepoRef, tag, target, name, body string) (*Release, error) {
	in := map[string]any{"tag_name": tag, "name": name, "body": body, "draft": true, "prerelease": false}
	if target != "" {
		in["target_commitish"] = target
	}
	var g struct {
		ID      int64  `json:"id"`
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Draft   bool   `json:"draft"`
		HTMLURL string `json:"html_url"`
	}
	if err := c.send(ctx, http.MethodPost, repoPath(repo)+"/releases", in, &g); err != nil {
		return nil, err
	}
	return &Release{ID: g.ID, TagName: g.TagName, Name: g.Name, Draft: g.Draft, HTMLURL: g.HTMLURL}, nil
}
