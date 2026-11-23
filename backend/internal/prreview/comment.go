package prreview

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/github"
)

// Marker identifies gitpulse's comment. Model output is sanitised so it can
// never contain "<!--", which means only gitpulse can produce this marker.
const Marker = "<!-- gitpulse:pr-review -->"

// Result is what gets stored per review and rendered in the UI and comment.
type Result struct {
	Analysis
	HeadSHA string    `json:"headSha"`
	AI      *AIReview `json:"ai,omitempty"`
}

// RenderComment builds the PR comment body. No-AI output has no summary.
func RenderComment(r Result, aiNote string) string {
	var b strings.Builder
	b.WriteString(Marker + "\n")
	fmt.Fprintf(&b, "### gitpulse review · `%s`\n\n", short(r.HeadSHA))
	fmt.Fprintf(&b, "**%d %s changed** · +%d −%d\n\n", r.FilesChanged, plural(r.FilesChanged, "file", "files"), r.Additions, r.Deletions)

	if r.AI != nil && r.AI.Summary != "" {
		b.WriteString("#### Summary\n" + r.AI.Summary + "\n\n")
	}
	if len(r.Flags) > 0 || (r.AI != nil && len(r.AI.Notable) > 0) {
		b.WriteString("#### Notable changes\n")
		for _, f := range r.Flags {
			fmt.Fprintf(&b, "- **%s:** %s", f.Title, codeList(f.Files))
			if f.Extra > 0 {
				fmt.Fprintf(&b, " and %d more", f.Extra)
			}
			b.WriteString("\n")
		}
		if r.AI != nil {
			for _, n := range r.AI.Notable {
				fmt.Fprintf(&b, "- %s\n", n)
			}
		}
		b.WriteString("\n")
	}
	if r.AI != nil && r.AI.SuggestedTitle != "" {
		b.WriteString("#### Suggested title\n" + "`" + strings.ReplaceAll(r.AI.SuggestedTitle, "`", "'") + "`\n\n")
	}
	labels := r.SuggestedLabels
	if r.AI != nil && len(r.AI.SuggestedLabels) > 0 {
		labels = mergeUnique(labels, r.AI.SuggestedLabels)
	}
	if len(labels) > 0 {
		b.WriteString("#### Suggested labels\n" + codeList(labels) + "\n\n")
	}
	if len(r.Omitted) > 0 {
		b.WriteString("<details><summary>Left out of the review (" + fmt.Sprint(len(r.Omitted)) + ")</summary>\n\n")
		for i, o := range r.Omitted {
			if i == 30 {
				fmt.Fprintf(&b, "- …and %d more\n", len(r.Omitted)-30)
				break
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", tick(o.Path), o.Reason)
		}
		b.WriteString("\n</details>\n\n")
	}
	foot := "Suggestions only: gitpulse never changes titles or labels. This comment updates on each push."
	if aiNote != "" {
		foot = aiNote + " " + foot
	}
	b.WriteString("<sub>" + foot + "</sub>\n")
	return b.String()
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func tick(s string) string { return strings.ReplaceAll(s, "`", "'") }

func codeList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "`" + tick(s) + "`"
	}
	return strings.Join(q, ", ")
}

func mergeUnique(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if k := strings.ToLower(s); !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// Commenter is the slice of the GitHub client needed to keep one comment.
type Commenter interface {
	IssueComments(ctx context.Context, repo github.RepoRef, number int) ([]github.IssueComment, error)
	CreateIssueComment(ctx context.Context, repo github.RepoRef, number int, body string) (*github.IssueComment, error)
	UpdateIssueComment(ctx context.Context, repo github.RepoRef, commentID int64, body string) (*github.IssueComment, error)
}

// UpsertComment keeps exactly one gitpulse comment on the PR:
//  1. edit the comment we stored last time;
//  2. if it was deleted (404) or none is stored, find our marked comment
//     (authored by the app's bot account) and edit it;
//  3. only if none exists, create one.
func UpsertComment(ctx context.Context, c Commenter, repo github.RepoRef, number int, botLogin string, storedID int64, body string) (*github.IssueComment, error) {
	if storedID != 0 {
		ic, err := c.UpdateIssueComment(ctx, repo, storedID, body)
		if err == nil {
			return ic, nil
		}
		var ae *github.APIError
		if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
			return nil, fmt.Errorf("update comment: %w", err)
		}
	}
	existing, err := c.IssueComments(ctx, repo, number)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	for _, ec := range existing {
		if strings.EqualFold(ec.Author, botLogin) && strings.Contains(ec.Body, Marker) {
			ic, err := c.UpdateIssueComment(ctx, repo, ec.ID, body)
			if err != nil {
				return nil, fmt.Errorf("update comment: %w", err)
			}
			return ic, nil
		}
	}
	ic, err := c.CreateIssueComment(ctx, repo, number, body)
	if err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	return ic, nil
}
