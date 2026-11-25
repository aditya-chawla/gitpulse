package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// User looks a user up by node ID. Installation tokens have no "viewer", and
// the node ID (unlike the login) never changes.
func (c *HTTPClient) User(ctx context.Context, nodeID string) (*Viewer, error) {
	var out struct {
		Node *struct {
			DatabaseID int64  `json:"databaseId"`
			ID         string `json:"id"`
			Login      string `json:"login"`
		} `json:"node"`
	}
	q := `query($id: ID!) { node(id: $id) { ... on User { databaseId id login } } }`
	if err := c.graphql(ctx, q, map[string]any{"id": nodeID}, &out); err != nil {
		return nil, err
	}
	if out.Node == nil || out.Node.DatabaseID == 0 {
		return nil, fmt.Errorf("user %s not found", nodeID)
	}
	return &Viewer{ID: out.Node.DatabaseID, NodeID: out.Node.ID, Login: out.Node.Login}, nil
}

const ownedReposQuery = `
query($login: String!, $n: Int!, $after: String) {
  user(login: $login) {
    repositories(first: $n, after: $after, ownerAffiliations: [OWNER], orderBy: {field: PUSHED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        databaseId id name owner { login } description visibility isFork isArchived isEmpty
        stargazerCount forkCount diskUsage createdAt pushedAt
        defaultBranchRef { name }
        licenseInfo { spdxId }
        repositoryTopics(first: 20) { nodes { topic { name } } }
        languages(first: 30, orderBy: {field: SIZE, direction: DESC}) { edges { size node { name } } }
        pullRequests(states: OPEN, first: 50, orderBy: {field: CREATED_AT, direction: ASC}) {
          totalCount
          nodes {
            number title url isDraft createdAt updatedAt author { login }
            reviewRequests(first: 10) { nodes { requestedReviewer { ... on User { login } } } }
            assignees(first: 5) { nodes { login } }
          }
        }
        issues(states: OPEN, first: 50, orderBy: {field: CREATED_AT, direction: DESC}) {
          totalCount
          nodes { number title url createdAt updatedAt author { login } assignees(first: 5) { nodes { login } } }
        }
        refs(refPrefix: "refs/heads/", first: 100) {
          totalCount
          nodes { name target { ... on Commit { committedDate } } }
        }
        forks(first: 20, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { createdAt owner { login } } }
      }
    }
  }
}`

type login struct {
	Login string `json:"login"`
}

type gqlRepo struct {
	DatabaseID       int64      `json:"databaseId"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Owner            login      `json:"owner"`
	Description      *string    `json:"description"`
	Visibility       string     `json:"visibility"`
	IsFork           bool       `json:"isFork"`
	IsArchived       bool       `json:"isArchived"`
	IsEmpty          bool       `json:"isEmpty"`
	StargazerCount   int        `json:"stargazerCount"`
	ForkCount        int        `json:"forkCount"`
	DiskUsage        int        `json:"diskUsage"`
	CreatedAt        time.Time  `json:"createdAt"`
	PushedAt         *time.Time `json:"pushedAt"`
	DefaultBranchRef *struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
	LicenseInfo *struct {
		SpdxID string `json:"spdxId"`
	} `json:"licenseInfo"`
	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string `json:"name"`
			} `json:"topic"`
		} `json:"nodes"`
	} `json:"repositoryTopics"`
	Languages struct {
		Edges []struct {
			Size int64 `json:"size"`
			Node struct {
				Name string `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"languages"`
	PullRequests struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			gqlItem
			IsDraft        bool `json:"isDraft"`
			ReviewRequests struct {
				Nodes []struct {
					RequestedReviewer *login `json:"requestedReviewer"`
				} `json:"nodes"`
			} `json:"reviewRequests"`
		} `json:"nodes"`
	} `json:"pullRequests"`
	Issues struct {
		TotalCount int       `json:"totalCount"`
		Nodes      []gqlItem `json:"nodes"`
	} `json:"issues"`
	Refs struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Name   string `json:"name"`
			Target struct {
				CommittedDate *time.Time `json:"committedDate"`
			} `json:"target"`
		} `json:"nodes"`
	} `json:"refs"`
	Forks struct {
		Nodes []struct {
			CreatedAt time.Time `json:"createdAt"`
			Owner     login     `json:"owner"`
		} `json:"nodes"`
	} `json:"forks"`
}

type gqlItem struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    *login    `json:"author"` // null for deleted ("ghost") users
	Assignees struct {
		Nodes []login `json:"nodes"`
	} `json:"assignees"`
}

func (g gqlItem) toItem() Item {
	it := Item{Number: g.Number, Title: g.Title, URL: g.URL, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
	if g.Author != nil {
		it.Author = g.Author.Login
	}
	for _, a := range g.Assignees.Nodes {
		it.Assignees = append(it.Assignees, a.Login)
	}
	return it
}

func (g *gqlRepo) toRepo() Repo {
	r := Repo{
		ID: g.DatabaseID, NodeID: g.ID, Ref: RepoRef{Owner: g.Owner.Login, Name: g.Name},
		Visibility: strings.ToLower(g.Visibility), IsFork: g.IsFork, IsArchived: g.IsArchived, IsEmpty: g.IsEmpty,
		Stars: g.StargazerCount, Forks: g.ForkCount, SizeKB: g.DiskUsage,
		CreatedAt: g.CreatedAt, PushedAt: g.PushedAt,
		OpenPRCount: g.PullRequests.TotalCount, OpenIssueCount: g.Issues.TotalCount,
		BranchesTruncated: g.Refs.TotalCount > len(g.Refs.Nodes),
	}
	if g.Description != nil {
		r.Description = *g.Description
	}
	if g.DefaultBranchRef != nil {
		r.DefaultBranch = g.DefaultBranchRef.Name
	}
	// GitHub reports "NOASSERTION" for licenses it can't identify; that still
	// means a license file exists.
	if g.LicenseInfo != nil {
		r.LicenseSPDX = g.LicenseInfo.SpdxID
	}
	for _, t := range g.RepositoryTopics.Nodes {
		r.Topics = append(r.Topics, t.Topic.Name)
	}
	for _, e := range g.Languages.Edges {
		r.Languages = append(r.Languages, LanguageBytes{Language: e.Node.Name, Bytes: e.Size})
	}
	for _, p := range g.PullRequests.Nodes {
		it := p.gqlItem.toItem()
		it.IsDraft = p.IsDraft
		for _, rr := range p.ReviewRequests.Nodes {
			if rr.RequestedReviewer != nil && rr.RequestedReviewer.Login != "" {
				it.ReviewRequests = append(it.ReviewRequests, rr.RequestedReviewer.Login)
			}
		}
		r.OpenPRs = append(r.OpenPRs, it)
	}
	for _, i := range g.Issues.Nodes {
		r.OpenIssues = append(r.OpenIssues, i.toItem())
	}
	for _, b := range g.Refs.Nodes {
		r.Branches = append(r.Branches, Branch{Name: b.Name, LastCommitAt: b.Target.CommittedDate})
	}
	for _, f := range g.Forks.Nodes {
		r.RecentForks = append(r.RecentForks, Actor{Login: f.Owner.Login, At: f.CreatedAt})
	}
	return r
}

// shrinkable reports whether a failed GraphQL call might succeed with a
// smaller page (server-side timeouts and resource limits).
func shrinkable(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status >= 500 {
		return true
	}
	var ge *GraphQLError
	return errors.As(err, &ge)
}

func (c *HTTPClient) OwnedRepos(ctx context.Context, login string) ([]Repo, error) {
	var (
		repos []Repo
		after *string
		size  = 20
	)
	for page := 0; page < 100; page++ {
		var out struct {
			User *struct {
				Repositories struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []gqlRepo `json:"nodes"`
				} `json:"repositories"`
			} `json:"user"`
		}
		err := c.graphql(ctx, ownedReposQuery, map[string]any{"login": login, "n": size, "after": after}, &out)
		if err != nil {
			if size > 1 && shrinkable(err) {
				size /= 2
				c.log.Warn("shrinking repo page size after error", "size", size, "err", err)
				continue
			}
			return nil, fmt.Errorf("list repos: %w", err)
		}
		if out.User == nil {
			return nil, fmt.Errorf("list repos: user %s not found", login)
		}
		for i := range out.User.Repositories.Nodes {
			repos = append(repos, out.User.Repositories.Nodes[i].toRepo())
		}
		pi := out.User.Repositories.PageInfo
		if !pi.HasNextPage {
			return repos, nil
		}
		cur := pi.EndCursor
		after = &cur
	}
	return repos, nil
}

// ErrPrivateRepo is returned when a watched repository is private.
var ErrPrivateRepo = errors.New("private repositories cannot be watched")

const publicRepoQuery = `
query($o: String!, $n: String!) {
  repository(owner: $o, name: $n) {
    databaseId id name owner { login } description visibility isFork isArchived isEmpty
    stargazerCount forkCount diskUsage createdAt pushedAt
    defaultBranchRef { name }
    licenseInfo { spdxId }
    repositoryTopics(first: 20) { nodes { topic { name } } }
    languages(first: 30, orderBy: {field: SIZE, direction: DESC}) { edges { size node { name } } }
    pullRequests(states: OPEN, first: 100, orderBy: {field: CREATED_AT, direction: ASC}) {
      totalCount
      nodes {
        number title url isDraft createdAt updatedAt author { login }
        reviewRequests(first: 10) { nodes { requestedReviewer { ... on User { login } } } }
        assignees(first: 5) { nodes { login } }
      }
    }
    issues(states: OPEN, first: 50, orderBy: {field: CREATED_AT, direction: DESC}) {
      totalCount
      nodes { number title url createdAt updatedAt author { login } assignees(first: 5) { nodes { login } } }
    }
    refs(refPrefix: "refs/heads/", first: 100) {
      totalCount
      nodes { name target { ... on Commit { committedDate } } }
    }
  }
}`

// PublicRepo fetches a single public repository by owner/name. When useGraphQL
// is true it uses GraphQL; otherwise it falls back to unauthenticated REST.
func (c *HTTPClient) PublicRepo(ctx context.Context, owner, name string, useGraphQL bool) (*Repo, error) {
	if useGraphQL {
		var out struct {
			Repository *gqlRepo `json:"repository"`
		}
		if err := c.graphql(ctx, publicRepoQuery, map[string]any{"o": owner, "n": name}, &out); err != nil {
			return nil, fmt.Errorf("public repo %s/%s: %w", owner, name, err)
		}
		if out.Repository == nil {
			return nil, &APIError{Status: http.StatusNotFound, URL: c.graphqlURL, Body: fmt.Sprintf("repository %s/%s not found", owner, name)}
		}
		r := out.Repository.toRepo()
		if r.Visibility == "private" {
			return nil, ErrPrivateRepo
		}
		return &r, nil
	}

	ref := RepoRef{Owner: owner, Name: name}
	var raw struct {
		ID              int64      `json:"id"`
		NodeID          string     `json:"node_id"`
		Name            string     `json:"name"`
		FullName        string     `json:"full_name"`
		Private         bool       `json:"private"`
		Visibility      string     `json:"visibility"`
		Description     *string    `json:"description"`
		Fork            bool       `json:"fork"`
		Archived        bool       `json:"archived"`
		Size            int        `json:"size"`
		StargazersCount int        `json:"stargazers_count"`
		ForksCount      int        `json:"forks_count"`
		DefaultBranch   string     `json:"default_branch"`
		Topics          []string   `json:"topics"`
		OpenIssuesCount int        `json:"open_issues_count"`
		CreatedAt       time.Time  `json:"created_at"`
		PushedAt        *time.Time `json:"pushed_at"`
		Owner           login      `json:"owner"`
		License         *struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := c.getJSON(ctx, repoPath(ref), &raw); err != nil {
		return nil, err
	}
	if raw.Private || strings.EqualFold(raw.Visibility, "private") {
		return nil, ErrPrivateRepo
	}
	vis := strings.ToLower(raw.Visibility)
	if vis == "" {
		vis = "public"
	}
	r := &Repo{
		ID: raw.ID, NodeID: raw.NodeID, Ref: RepoRef{Owner: raw.Owner.Login, Name: raw.Name},
		Visibility: vis, IsFork: raw.Fork, IsArchived: raw.Archived, IsEmpty: raw.Size == 0,
		Stars: raw.StargazersCount, Forks: raw.ForksCount, SizeKB: raw.Size,
		DefaultBranch: raw.DefaultBranch, Topics: raw.Topics,
		CreatedAt: raw.CreatedAt, PushedAt: raw.PushedAt,
	}
	if raw.Description != nil {
		r.Description = *raw.Description
	}
	if raw.License != nil {
		r.LicenseSPDX = raw.License.SPDXID
	}

	var langMap map[string]int64
	if err := c.getJSON(ctx, repoPath(ref)+"/languages", &langMap); err == nil {
		for lang, bytes := range langMap {
			r.Languages = append(r.Languages, LanguageBytes{Language: lang, Bytes: bytes})
		}
	}

	var rawBranches []struct {
		Name string `json:"name"`
	}
	if err := c.getJSON(ctx, repoPath(ref)+"/branches?per_page=100", &rawBranches); err == nil {
		for _, b := range rawBranches {
			r.Branches = append(r.Branches, Branch{Name: b.Name})
		}
	}

	var rawPRs []ghPR
	if err := c.getJSON(ctx, repoPath(ref)+"/pulls?state=open&sort=created&direction=asc&per_page=100", &rawPRs); err == nil {
		r.OpenPRCount = len(rawPRs)
		for _, p := range rawPRs {
			pr := p.toPR()
			r.OpenPRs = append(r.OpenPRs, Item{
				Number: pr.Number, Title: pr.Title, Author: pr.Author, URL: pr.URL,
				IsDraft: pr.Draft, CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt,
			})
		}
	}
	r.OpenIssueCount = max(0, raw.OpenIssuesCount-r.OpenPRCount)
	return r, nil
}

// treeFragment builds a nested Tree fragment `depth` levels deep.
func treeFragment(depth int) string {
	inner := "entries { name type }"
	for i := 1; i < depth; i++ {
		inner = "entries { name type object { ... on Tree { " + inner + " } } }"
	}
	return "fragment T on Tree { " + inner + " }"
}

type gqlTree struct {
	Entries []struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		Object *gqlTree `json:"object"`
	} `json:"entries"`
}

func flatten(prefix string, t *gqlTree, out *[]TreeEntry) {
	if t == nil {
		return
	}
	for _, e := range t.Entries {
		p := e.Name
		if prefix != "" {
			p = prefix + "/" + e.Name
		}
		*out = append(*out, TreeEntry{Path: p, IsDir: e.Type == "tree"})
		if e.Type == "tree" {
			flatten(p, e.Object, out)
		}
	}
}

func (c *HTTPClient) Trees(ctx context.Context, repos []RepoRef, depth int) (map[RepoRef][]TreeEntry, error) {
	out := map[RepoRef][]TreeEntry{}
	frag := treeFragment(depth)
	batch := 5
	for start := 0; start < len(repos); {
		end := min(start+batch, len(repos))
		chunk := repos[start:end]
		var q strings.Builder
		vars := map[string]any{}
		q.WriteString("query(")
		for i := range chunk {
			fmt.Fprintf(&q, "$o%d: String!, $n%d: String!, ", i, i)
			vars[fmt.Sprintf("o%d", i)] = chunk[i].Owner
			vars[fmt.Sprintf("n%d", i)] = chunk[i].Name
		}
		q.WriteString(") {")
		for i := range chunk {
			fmt.Fprintf(&q, ` r%d: repository(owner: $o%d, name: $n%d) { object(expression: "HEAD:") { ...T } }`, i, i, i)
		}
		q.WriteString(" }\n" + frag)

		var res map[string]*struct {
			Object *gqlTree `json:"object"`
		}
		if err := c.graphql(ctx, q.String(), vars, &res); err != nil {
			if batch > 1 && shrinkable(err) {
				batch = max(1, batch/2)
				continue
			}
			return nil, fmt.Errorf("trees: %w", err)
		}
		for i, ref := range chunk {
			r := res[fmt.Sprintf("r%d", i)]
			if r == nil || r.Object == nil {
				continue
			}
			var entries []TreeEntry
			flatten("", r.Object, &entries)
			out[ref] = entries
		}
		start = end
	}
	return out, nil
}

func (c *HTTPClient) FileContents(ctx context.Context, files []FileRef) (map[FileRef]string, error) {
	out := map[FileRef]string{}
	const batch = 40
	for start := 0; start < len(files); start += batch {
		chunk := files[start:min(start+batch, len(files))]
		var q strings.Builder
		vars := map[string]any{}
		q.WriteString("query(")
		for i := range chunk {
			fmt.Fprintf(&q, "$o%d: String!, $n%d: String!, $e%d: String!, ", i, i, i)
			vars[fmt.Sprintf("o%d", i)] = chunk[i].Repo.Owner
			vars[fmt.Sprintf("n%d", i)] = chunk[i].Repo.Name
			vars[fmt.Sprintf("e%d", i)] = "HEAD:" + chunk[i].Path
		}
		q.WriteString(") {")
		for i := range chunk {
			fmt.Fprintf(&q, ` f%d: repository(owner: $o%d, name: $n%d) { object(expression: $e%d) { ... on Blob { text isBinary } } }`, i, i, i, i)
		}
		q.WriteString(" }")
		var res map[string]*struct {
			Object *struct {
				Text     *string `json:"text"`
				IsBinary bool    `json:"isBinary"`
			} `json:"object"`
		}
		if err := c.graphql(ctx, q.String(), vars, &res); err != nil {
			return nil, fmt.Errorf("file contents: %w", err)
		}
		for i, f := range chunk {
			r := res[fmt.Sprintf("f%d", i)]
			if r == nil || r.Object == nil || r.Object.IsBinary || r.Object.Text == nil {
				continue
			}
			out[f] = *r.Object.Text
		}
	}
	return out, nil
}

const commitsQuery = `
query($o: String!, $n: String!, $aid: ID!, $since: GitTimestamp!, $after: String) {
  repository(owner: $o, name: $n) {
    defaultBranchRef { target { ... on Commit {
      history(first: 100, after: $after, since: $since, author: {id: $aid}) {
        pageInfo { hasNextPage endCursor }
        nodes { oid authoredDate additions deletions }
      }
    } } }
  }
}`

func (c *HTTPClient) CommitsBy(ctx context.Context, repo RepoRef, authorNodeID string, since time.Time) ([]Commit, error) {
	var (
		out   []Commit
		after *string
	)
	for page := 0; page < 50; page++ { // cap: 5000 commits per repo per scan
		var res struct {
			Repository *struct {
				DefaultBranchRef *struct {
					Target struct {
						History *struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								OID          string    `json:"oid"`
								AuthoredDate time.Time `json:"authoredDate"`
								Additions    int       `json:"additions"`
								Deletions    int       `json:"deletions"`
							} `json:"nodes"`
						} `json:"history"`
					} `json:"target"`
				} `json:"defaultBranchRef"`
			} `json:"repository"`
		}
		vars := map[string]any{"o": repo.Owner, "n": repo.Name, "aid": authorNodeID,
			"since": since.UTC().Format(time.RFC3339), "after": after}
		if err := c.graphql(ctx, commitsQuery, vars, &res); err != nil {
			return nil, fmt.Errorf("commits %s: %w", repo, err)
		}
		if res.Repository == nil || res.Repository.DefaultBranchRef == nil || res.Repository.DefaultBranchRef.Target.History == nil {
			return out, nil
		}
		h := res.Repository.DefaultBranchRef.Target.History
		for _, n := range h.Nodes {
			out = append(out, Commit{SHA: n.OID, AuthoredAt: n.AuthoredDate, Additions: n.Additions, Deletions: n.Deletions})
		}
		if !h.PageInfo.HasNextPage {
			return out, nil
		}
		cur := h.PageInfo.EndCursor
		after = &cur
	}
	return out, nil
}

func repoPath(r RepoRef) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

func (c *HTTPClient) CommitActivity(ctx context.Context, repo RepoRef) ([]CommitWeek, bool, error) {
	path := repoPath(repo) + "/stats/commit_activity"
	for attempt := 0; attempt < 4; attempt++ {
		status, body, err := c.restGet(ctx, path)
		if err != nil {
			if IsNotFound(err) {
				return nil, true, nil
			}
			return nil, false, err
		}
		switch status {
		case http.StatusNoContent:
			return nil, true, nil // empty repo
		case http.StatusAccepted:
			// GitHub is computing the stats in the background; ask again shortly.
			if err := c.sleep(ctx, time.Duration(2+attempt*2)*time.Second); err != nil {
				return nil, false, err
			}
			continue
		}
		var raw []struct {
			Days  [7]int `json:"days"`
			Total int    `json:"total"`
			Week  int64  `json:"week"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, false, fmt.Errorf("decode commit activity %s: %w", repo, err)
		}
		weeks := make([]CommitWeek, 0, len(raw))
		for _, w := range raw {
			weeks = append(weeks, CommitWeek{WeekStart: time.Unix(w.Week, 0).UTC(), Total: w.Total, Days: w.Days})
		}
		return weeks, true, nil
	}
	return nil, false, nil
}

func (c *HTTPClient) FailedRuns(ctx context.Context, repo RepoRef, since time.Time) ([]WorkflowRun, error) {
	q := url.Values{
		"status":   {"failure"},
		"created":  {">=" + since.UTC().Format("2006-01-02")},
		"per_page": {"50"},
	}
	_, body, err := c.restGet(ctx, repoPath(repo)+"/actions/runs?"+q.Encode())
	if err != nil {
		if IsNotFound(err) {
			return nil, nil // Actions disabled
		}
		return nil, err
	}
	var res struct {
		WorkflowRuns []struct {
			ID         int64     `json:"id"`
			Name       string    `json:"name"`
			HeadBranch string    `json:"head_branch"`
			Conclusion string    `json:"conclusion"`
			HTMLURL    string    `json:"html_url"`
			CreatedAt  time.Time `json:"created_at"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode workflow runs %s: %w", repo, err)
	}
	out := make([]WorkflowRun, 0, len(res.WorkflowRuns))
	for _, r := range res.WorkflowRuns {
		out = append(out, WorkflowRun{ID: r.ID, Name: r.Name, Branch: r.HeadBranch, Conclusion: r.Conclusion, URL: r.HTMLURL, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// RecentCommitAuthors fetches a single page (up to 100) of recent commits
// since `since` for contributor concentration / bus-factor checks.
func (c *HTTPClient) RecentCommitAuthors(ctx context.Context, repo RepoRef, since time.Time, limit int) ([]CommitAuthor, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	q := url.Values{
		"since":    {since.UTC().Format(time.RFC3339)},
		"per_page": {fmt.Sprintf("%d", limit)},
	}
	_, body, err := c.restGet(ctx, repoPath(repo)+"/commits?"+q.Encode())
	if err != nil {
		var ae *APIError
		if IsNotFound(err) || (errors.As(err, &ae) && ae.Status == http.StatusConflict) {
			return nil, nil // empty repo or unavailable
		}
		return nil, err
	}
	var raw []struct {
		Author *login `json:"author"`
		Commit struct {
			Author struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode commits %s: %w", repo, err)
	}
	out := make([]CommitAuthor, 0, len(raw))
	for _, item := range raw {
		who := item.Commit.Author.Name
		if item.Author != nil && item.Author.Login != "" {
			who = item.Author.Login
		}
		if who == "" {
			continue
		}
		out = append(out, CommitAuthor{Login: who, At: item.Commit.Author.Date})
	}
	return out, nil
}
