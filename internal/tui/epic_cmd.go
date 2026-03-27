package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// TrackedEpic is the TUI's view of a tracked epic (maps from store.TrackedEpic).
type TrackedEpic struct {
	EpicKey   string
	Name      string
	Active    bool
	SortOrder int
}

// EpicSection describes a single epic section in the Epics tab.
type EpicSection struct {
	EpicKey string
	Name    string
}

// EpicLoader abstracts the store for loading epic data.
type EpicLoader interface {
	GetActiveTrackedEpics(ctx context.Context) ([]TrackedEpic, error)
	GetJiraIssuesBySource(ctx context.Context, source string) ([]JiraIssue, error)
}

// EpicManager abstracts the store for interactive epic management (add/toggle/remove).
type EpicManager interface {
	AddTrackedEpic(ctx context.Context, epicKey, name string) error
	SetEpicActive(ctx context.Context, epicKey string, active bool) error
	RemoveTrackedEpic(ctx context.Context, epicKey string) error
}

// EpicStats holds computed statistics for a single epic section.
type EpicStats struct {
	Total      int
	Open       int
	Done       int
	Blocked    int
	Unassigned int
	MyCount    int
}

// computeEpicStats computes summary statistics for a set of Jira items in an epic.
// Classification is based on StatusCat (Jira status category):
//   - "done"          → Done
//   - "indeterminate" → Open (in progress)
//   - "new"           → Open (to do)
//
// Items with Status containing "blocked" (case-insensitive) are counted as Blocked.
// Items with empty Assignee are counted as Unassigned.
// Items whose Assignee matches currentUser are counted as MyCount.
func computeEpicStats(items []JiraItem, currentUser string) EpicStats {
	var s EpicStats
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
			s.Unassigned++
		}
		if currentUser != "" && item.issue.Assignee == currentUser {
			s.MyCount++
		}
	}
	return s
}

// containsIgnoreCase checks if s contains substr, case-insensitively.
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// epicPartition holds items for a single epic section, split by assignee group.
type epicPartition struct {
	mine       []JiraItem
	unassigned []JiraItem
	others     []JiraItem
}

// allItems returns all items in order: mine, unassigned, others.
func (p epicPartition) allItems() []JiraItem {
	result := make([]JiraItem, 0, len(p.mine)+len(p.unassigned)+len(p.others))
	result = append(result, p.mine...)
	result = append(result, p.unassigned...)
	result = append(result, p.others...)
	return result
}

// visibleItems returns mine + unassigned (default view without others).
func (p epicPartition) visibleItems() []JiraItem {
	result := make([]JiraItem, 0, len(p.mine)+len(p.unassigned))
	result = append(result, p.mine...)
	result = append(result, p.unassigned...)
	return result
}

// partitionEpicItems splits items into mine, unassigned, and others groups.
func partitionEpicItems(items []JiraItem, currentUser string) epicPartition {
	var p epicPartition
	for _, item := range items {
		switch {
		case currentUser != "" && item.issue.Assignee == currentUser:
			p.mine = append(p.mine, item)
		case item.issue.Assignee == "":
			p.unassigned = append(p.unassigned, item)
		default:
			p.others = append(p.others, item)
		}
	}
	return p
}

// epicDataLoadedMsg is returned when epic data finishes loading from the store.
type epicDataLoadedMsg struct {
	gen      uint64
	sections []EpicSection
	items    [][]JiraItem // items[i] = issues for sections[i]
	err      error
}

// EpicRefreshMsg is sent by the poll goroutine to tell the TUI to reload epic data.
type EpicRefreshMsg struct{}

// loadEpicData returns a tea.Cmd that loads tracked epics and their child issues.
func (m Model) loadEpicData() tea.Cmd {
	loader := m.epicLoader
	if loader == nil {
		return nil
	}
	gen := m.epicGen
	return func() tea.Msg {
		ctx := context.Background()

		epics, err := loader.GetActiveTrackedEpics(ctx)
		if err != nil {
			return epicDataLoadedMsg{gen: gen, err: err}
		}

		sections := make([]EpicSection, len(epics))
		items := make([][]JiraItem, len(epics))

		for i, epic := range epics {
			sections[i] = EpicSection{
				EpicKey: epic.EpicKey,
				Name:    epic.Name,
			}
			source := "epic:" + epic.EpicKey
			issues, err := loader.GetJiraIssuesBySource(ctx, source)
			if err != nil {
				return epicDataLoadedMsg{gen: gen, err: err}
			}
			jiraItems := make([]JiraItem, len(issues))
			for j, issue := range issues {
				jiraItems[j] = NewJiraItem(issue)
			}
			items[i] = jiraItems
		}

		return epicDataLoadedMsg{
			gen:      gen,
			sections: sections,
			items:    items,
		}
	}
}

// rebuildEpicLists creates/resets the epicLists, epicCollapsed, epicShowOthers,
// and epicPartitions slices based on new data.
func (m *Model) rebuildEpicLists(items [][]JiraItem) {
	n := len(m.epicSections)

	// Preserve collapse state for sections that still exist.
	oldCollapsed := m.epicCollapsed
	m.epicCollapsed = make([]bool, n)
	for i := 0; i < n && i < len(oldCollapsed); i++ {
		m.epicCollapsed[i] = oldCollapsed[i]
	}

	// Preserve showOthers state for sections that still exist.
	oldShowOthers := m.epicShowOthers
	m.epicShowOthers = make([]bool, n)
	for i := 0; i < n && i < len(oldShowOthers); i++ {
		m.epicShowOthers[i] = oldShowOthers[i]
	}

	// Partition items by assignee group.
	m.epicPartitions = make([]epicPartition, n)
	for i := 0; i < n; i++ {
		if i < len(items) {
			m.epicPartitions[i] = partitionEpicItems(items[i], m.currentUser)
		}
	}

	// Create fresh lists using the custom epic delegate.
	delegate := newEpicDelegate()

	m.epicLists = make([]list.Model, n)
	for i := 0; i < n; i++ {
		l := list.New(nil, delegate, 0, 0)
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)
		l.SetShowHelp(false)
		if i < len(m.epicPartitions) {
			l.SetItems(m.epicListItems(i))
		}
		m.epicLists[i] = l
	}

	// Clamp section index.
	if m.epicSection >= n {
		m.epicSection = 0
	}
}

// epicGroupHeader is a non-interactive separator item inserted between groups
// in the epic list. It renders as a styled label line.
type epicGroupHeader struct {
	label string
}

func (h epicGroupHeader) Title() string       { return h.label }
func (h epicGroupHeader) Description() string { return "" }
func (h epicGroupHeader) FilterValue() string { return "" }

// epicListItems returns the list items for a given epic section based on
// the current showOthers state. Inserts group header separators between
// non-empty groups for visual separation.
func (m *Model) epicListItems(section int) []list.Item {
	if section >= len(m.epicPartitions) {
		return nil
	}
	p := m.epicPartitions[section]
	showOthers := section < len(m.epicShowOthers) && m.epicShowOthers[section]

	var result []list.Item

	if len(p.mine) > 0 {
		result = append(result, epicGroupHeader{
			label: formatGroupHeader("Mine", len(p.mine)),
		})
		for _, item := range p.mine {
			result = append(result, item)
		}
	}

	if len(p.unassigned) > 0 {
		result = append(result, epicGroupHeader{
			label: formatGroupHeader("Unassigned", len(p.unassigned)),
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

// maybeLoadJiraDetailForEpic loads Jira detail for the selected epic item.
func (m *Model) maybeLoadJiraDetailForEpic() tea.Cmd {
	if m.jiraDetailFetcher == nil || !m.detailReady {
		return nil
	}

	ji, ok := m.SelectedEpicItem()
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

// SelectedEpicItem returns the currently selected JiraItem in the Epics tab, if any.
func (m Model) SelectedEpicItem() (JiraItem, bool) {
	if m.activeTab != 4 || len(m.epicLists) == 0 {
		return JiraItem{}, false
	}
	if m.epicSection < 0 || m.epicSection >= len(m.epicLists) {
		return JiraItem{}, false
	}
	item := m.epicLists[m.epicSection].SelectedItem()
	if item == nil {
		return JiraItem{}, false
	}
	ji, ok := item.(JiraItem)
	return ji, ok
}

// epicAddedMsg is returned after an epic is added via the EpicManager.
type epicAddedMsg struct {
	key string
	err error
}

// epicToggledMsg is returned after an epic's active state is toggled.
type epicToggledMsg struct {
	key    string
	active bool
	err    error
}

// epicRemovedMsg is returned after an epic is removed.
type epicRemovedMsg struct {
	key string
	err error
}

// addEpic returns a tea.Cmd that adds a tracked epic and reloads data.
func addEpic(mgr EpicManager, key, name string) tea.Cmd {
	return func() tea.Msg {
		err := mgr.AddTrackedEpic(context.Background(), key, name)
		return epicAddedMsg{key: key, err: err}
	}
}

// toggleEpic returns a tea.Cmd that toggles an epic's active state.
func toggleEpic(mgr EpicManager, key string, active bool) tea.Cmd {
	return func() tea.Msg {
		err := mgr.SetEpicActive(context.Background(), key, active)
		return epicToggledMsg{key: key, active: active, err: err}
	}
}

// removeEpic returns a tea.Cmd that removes a tracked epic and its issues.
func removeEpic(mgr EpicManager, key string) tea.Cmd {
	return func() tea.Msg {
		err := mgr.RemoveTrackedEpic(context.Background(), key)
		return epicRemovedMsg{key: key, err: err}
	}
}
