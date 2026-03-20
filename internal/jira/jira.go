package jira

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps the acli CLI for Jira operations.
type Client struct {
	acliPath string
}

// NewClient creates a new Jira client using the acli CLI at the given path.
func NewClient(acliPath string) *Client {
	return &Client{acliPath: acliPath}
}

// SearchByFilter searches for issues using a saved Jira filter ID.
func (c *Client) SearchByFilter(filterID int) ([]Issue, error) {
	args := []string{
		"jira", "workitem", "search",
		"--filter", fmt.Sprintf("%d", filterID),
		"--json", "--paginate",
		"--fields", "key,summary,status,priority,assignee,reporter,labels,issuetype",
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, fmt.Errorf("search by filter %d: %w", filterID, err)
	}
	return parseIssueJSON(out)
}

// SearchByJQL searches for issues using a JQL query string.
func (c *Client) SearchByJQL(jql string) ([]Issue, error) {
	args := []string{
		"jira", "workitem", "search",
		"--jql", jql,
		"--json", "--paginate",
		"--fields", "key,summary,status,priority,assignee,reporter,labels,issuetype",
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, fmt.Errorf("search by JQL: %w", err)
	}
	return parseIssueJSON(out)
}

// GetActiveSprintName finds the active sprint name for a project by searching
// for the scrum board and listing its active sprints.
func (c *Client) GetActiveSprintName(project string) (string, error) {
	// Step 1: Find scrum board for the project.
	boardOut, err := c.run("jira", "board", "search", "--project", project, "--type", "scrum", "--json")
	if err != nil {
		return "", fmt.Errorf("search boards for %s: %w", project, err)
	}

	var boardResp struct {
		Values []struct {
			ID int `json:"id"`
		} `json:"values"`
	}
	if err := json.Unmarshal(boardOut, &boardResp); err != nil {
		return "", fmt.Errorf("parse board search: %w", err)
	}
	if len(boardResp.Values) == 0 {
		return "", nil
	}

	// Step 2: Get active sprint from the first scrum board.
	boardID := fmt.Sprintf("%d", boardResp.Values[0].ID)
	sprintOut, err := c.run("jira", "board", "list-sprints", "--id", boardID, "--state", "active", "--json")
	if err != nil {
		return "", fmt.Errorf("list active sprints for board %s: %w", boardID, err)
	}

	var sprintResp struct {
		Sprints []struct {
			Name string `json:"name"`
		} `json:"sprints"`
	}
	if err := json.Unmarshal(sprintOut, &sprintResp); err != nil {
		return "", fmt.Errorf("parse sprint list: %w", err)
	}
	if len(sprintResp.Sprints) == 0 {
		return "", nil
	}

	return sprintResp.Sprints[0].Name, nil
}

// GetIssueDetail fetches full detail for a single issue by key.
func (c *Client) GetIssueDetail(key string) (*IssueDetail, error) {
	args := []string{
		"jira", "workitem", "view", key,
		"--json",
		"--fields", "key,summary,status,priority,assignee,reporter,description,labels,comment,issuetype",
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, fmt.Errorf("get issue detail %s: %w", key, err)
	}

	var raw acliIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse issue detail %s: %w", key, err)
	}

	issue := convertIssue(raw)
	detail := &IssueDetail{Issue: issue}

	if raw.Fields.Description != nil {
		detail.Description = flattenADF(raw.Fields.Description)
	}

	if raw.Fields.Comment != nil {
		for _, c := range raw.Fields.Comment.Comments {
			author := ""
			if c.Author != nil {
				author = c.Author.DisplayName
			}
			body := ""
			if c.Body != nil {
				body = flattenADF(c.Body)
			}
			detail.Comments = append(detail.Comments, Comment{
				Author:    author,
				Body:      body,
				CreatedAt: c.Created,
			})
		}
	}

	return detail, nil
}

// ClaimIssue assigns the issue to the current user and transitions it to "In Progress".
func (c *Client) ClaimIssue(key string) error {
	// Step 1: assign to current user.
	_, err := c.run("jira", "workitem", "assign", "--key", key, "--assignee", "@me", "--yes")
	if err != nil {
		return fmt.Errorf("assign %s: %w", key, err)
	}

	// Step 2: transition to In Progress.
	_, err = c.run("jira", "workitem", "transition", "--key", key, "--status", "In Progress", "--yes")
	if err != nil {
		return fmt.Errorf("transition %s: %w", key, err)
	}

	return nil
}

// BrowseURL builds the browser URL for an issue.
func BrowseURL(baseURL, key string) string {
	return strings.TrimRight(baseURL, "/") + "/browse/" + key
}

// run executes acli with the given arguments and returns stdout.
func (c *Client) run(args ...string) ([]byte, error) {
	cmd := exec.Command(c.acliPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s", err, string(ee.Stderr))
		}
		return nil, err
	}
	return out, nil
}

// parseIssueJSON parses acli search JSON output (an array of issues) into Issue structs.
func parseIssueJSON(data []byte) ([]Issue, error) {
	var rawIssues []acliIssue
	if err := json.Unmarshal(data, &rawIssues); err != nil {
		return nil, fmt.Errorf("parse issues JSON: %w", err)
	}

	issues := make([]Issue, 0, len(rawIssues))
	for _, raw := range rawIssues {
		issues = append(issues, convertIssue(raw))
	}
	return issues, nil
}

// convertIssue maps an acliIssue to our Issue type.
func convertIssue(raw acliIssue) Issue {
	issue := Issue{
		Key:     raw.Key,
		Summary: raw.Fields.Summary,
		Labels:  raw.Fields.Labels,
	}

	if raw.Fields.Status != nil {
		issue.Status = raw.Fields.Status.Name
		if raw.Fields.Status.Category != nil {
			issue.StatusCat = raw.Fields.Status.Category.Key
		}
	}
	if raw.Fields.Priority != nil {
		issue.Priority = raw.Fields.Priority.Name
	}
	if raw.Fields.IssueType != nil {
		issue.IssueType = raw.Fields.IssueType.Name
	}
	if raw.Fields.Assignee != nil {
		issue.Assignee = raw.Fields.Assignee.DisplayName
	}
	if raw.Fields.Reporter != nil {
		issue.Reporter = raw.Fields.Reporter.DisplayName
	}

	return issue
}

// flattenADF recursively extracts plain text from Jira's Atlassian Document Format JSON.
// Handles: doc, paragraph, text, heading, bulletList, orderedList, listItem, codeBlock, hardBreak.
func flattenADF(data json.RawMessage) string {
	if data == nil {
		return ""
	}

	var node map[string]json.RawMessage
	if err := json.Unmarshal(data, &node); err != nil {
		// Might be a string literal.
		var s string
		if json.Unmarshal(data, &s) == nil {
			return s
		}
		return ""
	}

	var nodeType string
	if t, ok := node["type"]; ok {
		json.Unmarshal(t, &nodeType)
	}

	var b strings.Builder

	switch nodeType {
	case "text":
		if t, ok := node["text"]; ok {
			var text string
			json.Unmarshal(t, &text)
			b.WriteString(text)
		}
	case "hardBreak":
		b.WriteString("\n")
	case "heading":
		children := flattenADFContent(node["content"])
		b.WriteString(children)
		b.WriteString("\n")
	case "paragraph":
		children := flattenADFContent(node["content"])
		b.WriteString(children)
		b.WriteString("\n")
	case "bulletList", "orderedList":
		b.WriteString(flattenADFContent(node["content"]))
	case "listItem":
		children := flattenADFContent(node["content"])
		b.WriteString("- ")
		b.WriteString(strings.TrimRight(children, "\n"))
		b.WriteString("\n")
	case "codeBlock":
		children := flattenADFContent(node["content"])
		b.WriteString(children)
		b.WriteString("\n")
	default:
		// Recurse into any content array for unknown node types.
		b.WriteString(flattenADFContent(node["content"]))
	}

	return b.String()
}

// flattenADFContent flattens an ADF "content" array node.
func flattenADFContent(data json.RawMessage) string {
	if data == nil {
		return ""
	}

	var children []json.RawMessage
	if err := json.Unmarshal(data, &children); err != nil {
		return ""
	}

	var b strings.Builder
	for _, child := range children {
		b.WriteString(flattenADF(child))
	}
	return b.String()
}
