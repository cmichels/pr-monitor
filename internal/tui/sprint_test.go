package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSprintLoader implements SprintLoader for tests.
type mockSprintLoader struct {
	issues []JiraIssue
}

func (m *mockSprintLoader) GetJiraIssuesBySourcePrefix(_ context.Context, _ string) ([]JiraIssue, error) {
	return m.issues, nil
}

func TestSprintTab_Appears(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	assert.Equal(t, "Sprint", m.tabs[5])
}

func TestComputeSprintStats(t *testing.T) {
	items := []JiraItem{
		NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "done", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "indeterminate", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-3", StatusCat: "new", Assignee: ""}),
		NewJiraItem(JiraIssue{Key: "OP-4", StatusCat: "new", Assignee: "bob"}),
		NewJiraItem(JiraIssue{Key: "OP-5", StatusCat: "new", Status: "Blocked", Assignee: "alice"}),
	}

	stats := computeSprintStats(items, "alice")

	assert.Equal(t, 5, stats.Total)
	assert.Equal(t, 1, stats.Done)
	assert.Equal(t, 4, stats.Open)
	assert.Equal(t, 3, stats.Mine) // alice: done + indeterminate + blocked
	assert.Equal(t, 1, stats.UpForGrabs)
	assert.Equal(t, 1, stats.Blocked)
}

func TestSprintDataLoadedMsg_PopulatesList(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := sprintDataLoadedMsg{
		sprintName: "Sprint 24",
		items: []JiraItem{
			NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "indeterminate", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "new", Assignee: ""}),
			NewJiraItem(JiraIssue{Key: "OP-3", StatusCat: "new", Assignee: "bob"}),
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	assert.Equal(t, "Sprint 24", m.sprintName)
	assert.Equal(t, 3, m.sprintStats.Total)
	assert.Equal(t, 1, m.sprintStats.Mine)
	assert.Equal(t, 1, m.sprintStats.UpForGrabs)

	// List should have mine + unassigned + 2 group headers = 4.
	assert.Equal(t, 4, len(m.sprintList.Items()))

	// Others (bob) should be hidden by default.
	assert.False(t, m.sprintShowOthers)
	assert.Len(t, m.sprintPartition.others, 1)
}

func TestSprintShowOthers_Toggle(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := sprintDataLoadedMsg{
		items: []JiraItem{
			NewJiraItem(JiraIssue{Key: "OP-1", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-2", Assignee: ""}),
			NewJiraItem(JiraIssue{Key: "OP-3", Assignee: "bob"}),
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 5

	// Default: 2 items + 2 headers = 4.
	assert.Equal(t, 4, len(m.sprintList.Items()))

	// Press 'e' to show others.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)

	assert.True(t, m.sprintShowOthers)
	// 3 items + 3 headers = 6.
	assert.Equal(t, 6, len(m.sprintList.Items()))

	// Press 'e' again to hide.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)

	assert.False(t, m.sprintShowOthers)
	assert.Equal(t, 4, len(m.sprintList.Items()))
}

func TestSprintSorting_InProgressFirst(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := sprintDataLoadedMsg{
		items: []JiraItem{
			NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "new", Assignee: "alice"}),         // To Do
			NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "indeterminate", Assignee: "alice"}), // In Progress
			NewJiraItem(JiraIssue{Key: "OP-3", StatusCat: "new", Assignee: "alice"}),         // To Do
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Mine partition should have In Progress first, then To Do items.
	require.Len(t, m.sprintPartition.mine, 3)
	assert.Equal(t, "OP-2", m.sprintPartition.mine[0].issue.Key, "In Progress should sort first")
	// The remaining two are both "new", order preserved by stable sort.
	assert.Equal(t, "indeterminate", m.sprintPartition.mine[0].issue.StatusCat)
	assert.Equal(t, "new", m.sprintPartition.mine[1].issue.StatusCat)
	assert.Equal(t, "new", m.sprintPartition.mine[2].issue.StatusCat)
}

func TestSelectedSprintItem(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	// Not on Sprint tab: should return false.
	_, ok := m.SelectedSprintItem()
	assert.False(t, ok)

	// Load sprint data.
	msg := sprintDataLoadedMsg{
		items: []JiraItem{
			NewJiraItem(JiraIssue{Key: "OP-1", Assignee: "alice"}),
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 5

	// Index 0 is group header, index 1 is the item.
	m.sprintList.Select(1)
	item, ok := m.SelectedSprintItem()
	assert.True(t, ok)
	assert.Equal(t, "OP-1", item.issue.Key)
}

func TestSprintRefreshMsg_ReloadsData(t *testing.T) {
	loader := &mockSprintLoader{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithSprintLoader(loader),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// SprintRefreshMsg should trigger a reload.
	_, cmd := m.Update(SprintRefreshMsg{})
	assert.NotNil(t, cmd)
}

func TestSprintGroupHeaders(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := sprintDataLoadedMsg{
		items: []JiraItem{
			NewJiraItem(JiraIssue{Key: "OP-1", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-2", Assignee: ""}),
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	items := m.sprintList.Items()
	require.Equal(t, 4, len(items))

	// First: "My Work" header.
	h1, ok := items[0].(epicGroupHeader)
	assert.True(t, ok)
	assert.Contains(t, h1.label, "My Work")

	// Third: "Up for Grabs" header.
	h2, ok := items[2].(epicGroupHeader)
	assert.True(t, ok)
	assert.Contains(t, h2.label, "Up for Grabs")
}

func TestRenderSprintHeader(t *testing.T) {
	stats := SprintStats{Total: 20, Done: 8, Open: 12, Mine: 5, UpForGrabs: 4, Blocked: 1}
	header := renderSprintHeader("Sprint 24", stats)
	stripped := stripAnsi(header)

	assert.Contains(t, stripped, "Sprint 24")
	assert.Contains(t, stripped, "40%")
	assert.Contains(t, stripped, "8/20 done")
	assert.Contains(t, stripped, "5 mine")
	assert.Contains(t, stripped, "4 up for grabs")
	assert.Contains(t, stripped, "1 blocked")
}

func TestRenderSprintHeader_EmptyName(t *testing.T) {
	stats := SprintStats{Total: 5, Done: 2}
	header := renderSprintHeader("", stats)
	stripped := stripAnsi(header)

	assert.Contains(t, stripped, "Sprint")
	assert.Contains(t, stripped, "40%")
}
