// Package health scores a repository out of 100 and labels its activity.
// It is pure (no I/O) so the rules are easy to test and to show in the UI.
package health

import (
	"fmt"
	"time"
)

const (
	StaleBranchAge = 90 * 24 * time.Hour
	OldPRAge       = 30 * 24 * time.Hour

	ActiveWithin  = 30 * 24 * time.Hour
	SlowingWithin = 180 * 24 * time.Hour
)

type Status string

const (
	Active  Status = "active"
	Slowing Status = "slowing"
	Dormant Status = "dormant"
)

type Branch struct {
	Name         string
	LastCommitAt *time.Time
	IsDefault    bool
}

type Input struct {
	HasReadme      bool
	HasLicense     bool
	HasCI          bool
	HasTests       bool
	HasDescription bool
	TopicCount     int
	PushedAt       *time.Time
	Branches       []Branch
	OpenPRCreated  []time.Time
}

// Check is one line of the visible breakdown.
type Check struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Points int    `json:"points"`
	Max    int    `json:"max"`
	Detail string `json:"detail"`
}

type Result struct {
	Score  int     `json:"score"`
	Status Status  `json:"status"`
	Checks []Check `json:"checks"`
}

func boolCheck(key, label string, ok bool, max int, yes, no string) Check {
	c := Check{Key: key, Label: label, Max: max, Detail: no}
	if ok {
		c.Points, c.Detail = max, yes
	}
	return c
}

func Evaluate(in Input, now time.Time) Result {
	checks := []Check{
		boolCheck("readme", "README", in.HasReadme, 15, "README present", "No README"),
		boolCheck("license", "License", in.HasLicense, 10, "License present", "No license"),
		boolCheck("ci", "CI workflows", in.HasCI, 15, "GitHub Actions workflows found", "No CI workflows in .github/workflows"),
		boolCheck("tests", "Tests", in.HasTests, 15, "Tests found", "No test directory or test files found"),
		activityCheck(in.PushedAt, now),
		staleBranchCheck(in.Branches, now),
		oldPRCheck(in.OpenPRCreated, now),
		metadataCheck(in.HasDescription, in.TopicCount),
	}
	r := Result{Status: StatusFor(in.PushedAt, now), Checks: checks}
	for _, c := range checks {
		r.Score += c.Points
	}
	return r
}

func StatusFor(pushedAt *time.Time, now time.Time) Status {
	if pushedAt == nil {
		return Dormant
	}
	switch age := now.Sub(*pushedAt); {
	case age <= ActiveWithin:
		return Active
	case age <= SlowingWithin:
		return Slowing
	default:
		return Dormant
	}
}

func activityCheck(pushedAt *time.Time, now time.Time) Check {
	c := Check{Key: "activity", Label: "Recent activity", Max: 15}
	if pushedAt == nil {
		c.Detail = "Never pushed"
		return c
	}
	days := int(now.Sub(*pushedAt).Hours() / 24)
	switch {
	case days <= 30:
		c.Points = 15
	case days <= 90:
		c.Points = 8
	}
	c.Detail = fmt.Sprintf("Last push %d days ago", days)
	return c
}

func staleBranchCheck(branches []Branch, now time.Time) Check {
	c := Check{Key: "stale_branches", Label: "No stale branches", Max: 10}
	stale := 0
	for _, b := range branches {
		if !b.IsDefault && b.LastCommitAt != nil && now.Sub(*b.LastCommitAt) > StaleBranchAge {
			stale++
		}
	}
	c.Points = max(0, c.Max-3*stale)
	c.Detail = plural(stale, "stale branch", "stale branches") + " (no commits in 90+ days)"
	return c
}

func oldPRCheck(created []time.Time, now time.Time) Check {
	c := Check{Key: "old_prs", Label: "No old open PRs", Max: 10}
	old := 0
	for _, t := range created {
		if now.Sub(t) > OldPRAge {
			old++
		}
	}
	c.Points = max(0, c.Max-5*old)
	c.Detail = plural(old, "open PR", "open PRs") + " older than 30 days"
	return c
}

func metadataCheck(hasDesc bool, topics int) Check {
	c := Check{Key: "metadata", Label: "Description & topics", Max: 10}
	var missing []string
	if hasDesc {
		c.Points += 5
	} else {
		missing = append(missing, "description")
	}
	if topics > 0 {
		c.Points += 5
	} else {
		missing = append(missing, "topics")
	}
	switch len(missing) {
	case 0:
		c.Detail = "Description and topics set"
	case 1:
		c.Detail = "Missing " + missing[0]
	default:
		c.Detail = "Missing description and topics"
	}
	return c
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
