package tui

import (
	"context"
	"fmt"
	"strings"
)

// JiraIssue is the TUI's view of a Jira issue (maps from store.JiraIssue).
type JiraIssue struct {
	Key       string
	Summary   string
	Status    string
	StatusCat string
	Priority  string
	IssueType string
	Assignee  string
	Reporter  string
	Labels    string // JSON array string
	Source    string
	BrowseURL string
}

// JiraDetail contains on-demand detail for a selected Jira issue.
type JiraDetail struct {
	Key         string
	Summary     string
	Status      string
	Priority    string
	IssueType   string
	Assignee    string
	Reporter    string
	Labels      []string
	Description string
	Comments    []JiraComment
}

// JiraComment represents a comment on a Jira issue.
type JiraComment struct {
	Author    string
	Body      string
	CreatedAt string
}

// JiraItem implements the bubbles list.DefaultItem interface for displaying Jira issues.
type JiraItem struct {
	issue JiraIssue
}

// NewJiraItem creates a JiraItem from a JiraIssue.
func NewJiraItem(issue JiraIssue) JiraItem {
	return JiraItem{issue: issue}
}

func (i JiraItem) FilterValue() string {
	return i.issue.Key + " " + i.issue.Summary
}

func (i JiraItem) Title() string {
	return fmt.Sprintf("%-10s %s", i.issue.Key, i.issue.Summary)
}

func (i JiraItem) Description() string {
	parts := []string{i.issue.IssueType}

	if i.issue.Priority != "" {
		parts = append(parts, i.issue.Priority)
	}

	parts = append(parts, i.issue.Status)

	if i.issue.Assignee != "" {
		parts = append(parts, i.issue.Assignee)
	} else {
		parts = append(parts, "(unassigned)")
	}

	return strings.Join(parts, " | ")
}

// JiraLoader abstracts the store for loading Jira issues by source.
type JiraLoader interface {
	GetJiraIssuesBySource(ctx context.Context, source string) ([]JiraIssue, error)
}

// JiraDetailFetcher fetches on-demand Jira issue detail.
type JiraDetailFetcher interface {
	GetIssueDetail(ctx context.Context, key string) (*JiraDetail, error)
}

// JiraClaimer assigns a Jira issue to the current user and transitions it to In Progress.
type JiraClaimer interface {
	ClaimIssue(key string) error
}
