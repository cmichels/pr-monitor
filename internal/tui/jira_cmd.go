package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// jiraDataLoadedMsg is returned when Jira data finishes loading from the store.
type jiraDataLoadedMsg struct {
	gen         uint64
	inProgress  []JiraItem
	submissions []JiraItem
	knowledge   []JiraItem
	myTasks     []JiraItem
	currentUser string // inferred from my_tasks assignee
	err         error
}

// jiraDetailLoadedMsg is returned when a Jira issue detail fetch completes.
type jiraDetailLoadedMsg struct {
	key    string
	detail *JiraDetail
	err    error
}

// JiraRefreshMsg is sent by the poll goroutine to tell the TUI to reload Jira data.
type JiraRefreshMsg struct{}

// loadJiraData returns a tea.Cmd that loads Jira issues from the store by source.
// Items with status "In Progress" are partitioned into a dedicated section.
func (m Model) loadJiraData() tea.Cmd {
	loader := m.jiraLoader
	if loader == nil {
		return nil
	}
	sources := m.jiraSourceKeys()
	gen := m.jiraGen
	return func() tea.Msg {
		ctx := context.Background()
		result := jiraDataLoadedMsg{gen: gen}

		// Two-pass approach: collect all issues first, then partition.
		// We infer the current user from the my_tasks source (index 2)
		// since its JQL uses assignee=currentUser().
		type sourceItems struct {
			issues []JiraIssue
		}
		collected := make([]sourceItems, len(sources))
		currentUser := ""

		for i, source := range sources {
			issues, err := loader.GetJiraIssuesBySource(ctx, source)
			if err != nil {
				return jiraDataLoadedMsg{gen: gen, err: err}
			}
			collected[i].issues = issues
			// Learn current user from my_tasks (assignee=currentUser() in JQL).
			if i == 2 {
				for _, issue := range issues {
					if issue.Assignee != "" {
						currentUser = issue.Assignee
						break
					}
				}
			}
		}

		// Partition: items assigned to me with status "In Progress" go to
		// the dedicated section regardless of source.
		for i, src := range collected {
			for _, issue := range src.issues {
				item := NewJiraItem(issue)
				if currentUser != "" && issue.Status == "In Progress" && issue.Assignee == currentUser {
					result.inProgress = append(result.inProgress, item)
				} else {
					switch i {
					case 0:
						result.submissions = append(result.submissions, item)
					case 1:
						result.knowledge = append(result.knowledge, item)
					case 2:
						result.myTasks = append(result.myTasks, item)
					}
				}
			}
		}
		result.currentUser = currentUser
		return result
	}
}

// fetchJiraDetail returns a tea.Cmd that fetches detail for a Jira issue.
func fetchJiraDetail(fetcher JiraDetailFetcher, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		detail, err := fetcher.GetIssueDetail(ctx, key)
		return jiraDetailLoadedMsg{
			key:    key,
			detail: detail,
			err:    err,
		}
	}
}

// jiraClaimedMsg is returned when a Jira claim (assign + transition) completes.
type jiraClaimedMsg struct {
	key string
	err error
}

// claimJiraIssue returns a tea.Cmd that claims a Jira issue (assign to me + transition to In Progress).
func claimJiraIssue(claimer JiraClaimer, item JiraItem) tea.Cmd {
	key := item.issue.Key
	return func() tea.Msg {
		err := claimer.ClaimIssue(key)
		return jiraClaimedMsg{key: key, err: err}
	}
}

// launchWorktree opens a new tmux window and runs claude with /worktree for the issue.
func launchWorktree(item JiraItem) tea.Cmd {
	issueKey := item.issue.Key
	return func() tea.Msg {
		driver, err := DetectDriver("auto")
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Launch failed: %v", err)}
		}

		windowID, err := driver.SpawnWindow("")
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Failed to open window: %v", err)}
		}

		if windowID != "" {
			_ = driver.SetTitle(windowID, issueKey)
			time.Sleep(500 * time.Millisecond)
			worktreeCmd := fmt.Sprintf("claude --model claude-sonnet-4-6 '/worktree %s'", issueKey)
			_ = driver.SendText(windowID, worktreeCmd)
		}

		return statusMsg{text: fmt.Sprintf("Working on %s", issueKey)}
	}
}

// jiraSourceKeys returns the store source keys for the 3 Jira sections.
func (m Model) jiraSourceKeys() [3]string {
	return m.jiraSrcKeys
}
