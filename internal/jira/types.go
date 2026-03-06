package jira

import "encoding/json"

// Issue represents a Jira issue from a search result.
type Issue struct {
	Key       string
	Summary   string
	Status    string
	StatusCat string // "new", "indeterminate", "done"
	Priority  string
	IssueType string
	Assignee  string
	Reporter  string
	Labels    []string
}

// IssueDetail contains the full detail for a single issue including description and comments.
type IssueDetail struct {
	Issue
	Description string
	Comments    []Comment
}

// Comment represents a Jira issue comment.
type Comment struct {
	Author    string
	Body      string
	CreatedAt string
}

// acli JSON structures — these mirror the Jira REST API v3 response
// as returned by `acli jira workitem search --json` and `acli jira workitem view --json`.

type acliIssue struct {
	Key    string     `json:"key"`
	Self   string     `json:"self"`
	Fields acliFields `json:"fields"`
}

type acliFields struct {
	Summary     string          `json:"summary"`
	Status      *acliStatus     `json:"status"`
	Priority    *acliPriority   `json:"priority"`
	IssueType   *acliIssueType  `json:"issuetype"`
	Assignee    *acliUser       `json:"assignee"`
	Reporter    *acliUser       `json:"reporter"`
	Labels      []string        `json:"labels"`
	Description json.RawMessage `json:"description"`
	Comment     *acliComment    `json:"comment"`
}

type acliStatus struct {
	Name     string              `json:"name"`
	Category *acliStatusCategory `json:"statusCategory"`
}

type acliStatusCategory struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type acliPriority struct {
	Name string `json:"name"`
}

type acliIssueType struct {
	Name string `json:"name"`
}

type acliUser struct {
	DisplayName string `json:"displayName"`
}

type acliComment struct {
	Comments []acliCommentEntry `json:"comments"`
}

type acliCommentEntry struct {
	Author  *acliUser       `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
}
