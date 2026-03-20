package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// SprintLoader abstracts the store for loading sprint data.
type SprintLoader interface {
	GetJiraIssuesBySourcePrefix(ctx context.Context, prefix string) ([]JiraIssue, error)
}

// SprintStats holds computed statistics for the sprint.
type SprintStats struct {
	Total      int
	Done       int
	Open       int
	Mine       int
	UpForGrabs int
	Blocked    int
}

// computeSprintStats computes summary statistics for sprint items.
func computeSprintStats(items []JiraItem, currentUser string) SprintStats {
	var s SprintStats
	s.Total = len(items)
	for _, item := range items {
		switch item.issue.StatusCat {
		case "done":
			s.Done++
		default:
			s.Open++
		}
		if containsIgnoreCase(item.issue.Status, "blocked") {
			s.Blocked++
		}
		if item.issue.Assignee == "" {
			s.UpForGrabs++
		}
		if currentUser != "" && item.issue.Assignee == currentUser {
			s.Mine++
		}
	}
	return s
}

// sprintDataLoadedMsg is returned when sprint data finishes loading from the store.
type sprintDataLoadedMsg struct {
	items      []JiraItem
	sprintName string
	err        error
}

// SprintRefreshMsg is sent by the poll goroutine to tell the TUI to reload sprint data.
type SprintRefreshMsg struct{}

// loadSprintData returns a tea.Cmd that loads sprint issues from the store.
func (m Model) loadSprintData() tea.Cmd {
	loader := m.sprintLoader
	if loader == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()

		issues, err := loader.GetJiraIssuesBySourcePrefix(ctx, "sprint")
		if err != nil {
			return sprintDataLoadedMsg{err: err}
		}

		// Extract sprint name from the source field (e.g. "sprint:Sprint 24").
		sprintName := ""
		for _, issue := range issues {
			if strings.HasPrefix(issue.Source, "sprint:") {
				sprintName = strings.TrimPrefix(issue.Source, "sprint:")
				break
			}
		}

		items := make([]JiraItem, len(issues))
		for i, issue := range issues {
			items[i] = NewJiraItem(issue)
		}

		return sprintDataLoadedMsg{
			items:      items,
			sprintName: sprintName,
		}
	}
}

// rebuildSprintList creates/resets the sprint list based on new data.
func (m *Model) rebuildSprintList(items []JiraItem) {
	m.sprintPartition = partitionEpicItems(items, m.currentUser)

	// Sort "mine" items: In Progress first, then To Do.
	sort.SliceStable(m.sprintPartition.mine, func(i, j int) bool {
		return statusPriority(m.sprintPartition.mine[i].issue.StatusCat) <
			statusPriority(m.sprintPartition.mine[j].issue.StatusCat)
	})

	delegate := newEpicDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetItems(m.sprintListItems())
	m.sprintList = l
}

// sprintListItems returns the list items for the sprint based on showOthers state.
func (m *Model) sprintListItems() []list.Item {
	p := m.sprintPartition
	showOthers := m.sprintShowOthers

	var result []list.Item

	if len(p.mine) > 0 {
		result = append(result, epicGroupHeader{
			label: formatGroupHeader("My Work", len(p.mine)),
		})
		for _, item := range p.mine {
			result = append(result, item)
		}
	}

	if len(p.unassigned) > 0 {
		result = append(result, epicGroupHeader{
			label: formatGroupHeader("Up for Grabs", len(p.unassigned)),
		})
		for _, item := range p.unassigned {
			result = append(result, item)
		}
	}

	if showOthers && len(p.others) > 0 {
		result = append(result, epicGroupHeader{
			label: formatGroupHeader("Others", len(p.others)),
		})
		for _, item := range p.others {
			result = append(result, item)
		}
	}

	return result
}

// SelectedSprintItem returns the currently selected JiraItem in the Sprint tab, if any.
func (m Model) SelectedSprintItem() (JiraItem, bool) {
	if m.activeTab != 5 {
		return JiraItem{}, false
	}
	item := m.sprintList.SelectedItem()
	if item == nil {
		return JiraItem{}, false
	}
	ji, ok := item.(JiraItem)
	return ji, ok
}

// maybeLoadJiraDetailForSprint loads Jira detail for the selected sprint item.
func (m *Model) maybeLoadJiraDetailForSprint() tea.Cmd {
	if m.jiraDetailFetcher == nil || !m.detailReady {
		return nil
	}

	ji, ok := m.SelectedSprintItem()
	if !ok {
		m.jiraDetail = nil
		m.jiraDetailKey = ""
		m.jiraDetailLoading = false
		m.jiraDetailErr = nil
		m.updateDetailViewport()
		return nil
	}

	issueKey := ji.issue.Key
	if issueKey == m.jiraDetailKey && !m.jiraDetailLoading {
		return nil
	}

	m.jiraDetailKey = issueKey

	if cached, ok := m.jiraDetailCache[issueKey]; ok {
		m.jiraDetail = cached
		m.jiraDetailLoading = false
		m.jiraDetailErr = nil
		m.updateDetailViewport()
		return nil
	}

	m.jiraDetailLoading = true
	m.jiraDetailErr = nil
	m.jiraDetail = nil
	m.updateDetailViewport()
	return fetchJiraDetail(m.jiraDetailFetcher, issueKey)
}

// statusPriority returns a sort key for status categories.
// Lower values sort first: "indeterminate" (In Progress) before "new" (To Do).
func statusPriority(statusCat string) int {
	switch statusCat {
	case "indeterminate":
		return 0
	case "new":
		return 1
	case "done":
		return 2
	default:
		return 3
	}
}

// formatGroupHeader creates a consistent group header label.
func formatGroupHeader(name string, count int) string {
	return fmt.Sprintf("── %s (%d) ──", name, count)
}
