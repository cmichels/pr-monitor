package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEpicLoader implements EpicLoader for tests.
type mockEpicLoader struct {
	epics  []TrackedEpic
	issues map[string][]JiraIssue // keyed by source
}

func (m *mockEpicLoader) GetActiveTrackedEpics(_ context.Context) ([]TrackedEpic, error) {
	return m.epics, nil
}

func (m *mockEpicLoader) GetJiraIssuesBySource(_ context.Context, source string) ([]JiraIssue, error) {
	return m.issues[source], nil
}

func TestEpicsTab_Appears(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	assert.Equal(t, "Epics", m.tabs[4])
}

func TestEpicDataLoadedMsg_PopulatesSections(t *testing.T) {
	epicLoader := &mockEpicLoader{
		epics: []TrackedEpic{
			{EpicKey: "OP-3309", Name: "UX/UI Design 2026"},
		},
		issues: map[string][]JiraIssue{
			"epic:OP-3309": {
				{Key: "OP-101", Summary: "Design login page", Status: "To Do"},
				{Key: "OP-102", Summary: "Design dashboard", Status: "In Progress"},
			},
		},
	}

	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicLoader(epicLoader),
	)

	// Simulate WindowSizeMsg so the model is ready.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Simulate epicDataLoadedMsg.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-3309", Name: "UX/UI Design 2026"},
		},
		items: [][]JiraItem{
			{
				NewJiraItem(JiraIssue{Key: "OP-101", Summary: "Design login page", Status: "To Do"}),
				NewJiraItem(JiraIssue{Key: "OP-102", Summary: "Design dashboard", Status: "In Progress"}),
			},
		},
	}

	updated, _ = m.Update(msg)
	m = updated.(Model)

	assert.Len(t, m.epicSections, 1)
	assert.Equal(t, "OP-3309", m.epicSections[0].EpicKey)
	assert.Len(t, m.epicLists, 1)
	// 2 items + 1 "Unassigned" group header = 3 list items (no currentUser, so no "Mine" group).
	assert.Equal(t, 3, len(m.epicLists[0].Items()))
}

func TestEpicSectionSwitching(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Simulate two epic sections loaded.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-1", Name: "Epic 1"},
			{EpicKey: "OP-2", Name: "Epic 2"},
		},
		items: [][]JiraItem{
			{NewJiraItem(JiraIssue{Key: "OP-10", Summary: "Issue A", Status: "To Do"})},
			{NewJiraItem(JiraIssue{Key: "OP-20", Summary: "Issue B", Status: "To Do"})},
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Switch to Epics tab.
	m.activeTab = 4
	assert.Equal(t, 0, m.epicSection)

	// Press 's' to cycle section.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	assert.Equal(t, 1, m.epicSection)

	// Press 's' again to wrap back.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	assert.Equal(t, 0, m.epicSection)
}

func TestSelectedEpicItem(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// No epic data: should return false.
	_, ok := m.SelectedEpicItem()
	assert.False(t, ok)

	// Load epic data.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-1", Name: "Epic 1"},
		},
		items: [][]JiraItem{
			{NewJiraItem(JiraIssue{Key: "OP-10", Summary: "Issue A", Status: "To Do"})},
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Not on Epics tab: should return false.
	m.activeTab = 0
	_, ok = m.SelectedEpicItem()
	assert.False(t, ok)

	// On Epics tab: index 0 is the group header, select index 1 for the actual item.
	m.activeTab = 4
	m.epicLists[0].Select(1)
	item, ok := m.SelectedEpicItem()
	assert.True(t, ok)
	assert.Equal(t, "OP-10", item.issue.Key)
}

func TestComputeEpicStats(t *testing.T) {
	tests := []struct {
		name        string
		items       []JiraItem
		currentUser string
		want        EpicStats
	}{
		{
			name:  "empty items",
			items: nil,
			want:  EpicStats{},
		},
		{
			name: "mixed statuses",
			items: []JiraItem{
				NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "done", Assignee: "alice"}),
				NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "indeterminate", Assignee: "bob"}),
				NewJiraItem(JiraIssue{Key: "OP-3", StatusCat: "new", Assignee: ""}),
				NewJiraItem(JiraIssue{Key: "OP-4", StatusCat: "done", Assignee: "alice"}),
			},
			currentUser: "alice",
			want: EpicStats{
				Total:      4,
				Open:       2,
				Done:       2,
				Blocked:    0,
				Unassigned: 1,
				MyCount:    2,
			},
		},
		{
			name: "blocked items counted",
			items: []JiraItem{
				NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "indeterminate", Status: "Blocked", Assignee: "alice"}),
				NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "indeterminate", Status: "BLOCKED BY EXTERNAL", Assignee: "bob"}),
				NewJiraItem(JiraIssue{Key: "OP-3", StatusCat: "new", Status: "To Do", Assignee: "alice"}),
			},
			currentUser: "alice",
			want: EpicStats{
				Total:      3,
				Open:       3,
				Done:       0,
				Blocked:    2,
				Unassigned: 0,
				MyCount:    2,
			},
		},
		{
			name: "no current user skips my count",
			items: []JiraItem{
				NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "new", Assignee: "alice"}),
			},
			currentUser: "",
			want: EpicStats{
				Total:      1,
				Open:       1,
				Done:       0,
				Blocked:    0,
				Unassigned: 0,
				MyCount:    0,
			},
		},
		{
			name: "all done",
			items: []JiraItem{
				NewJiraItem(JiraIssue{Key: "OP-1", StatusCat: "done", Assignee: "alice"}),
				NewJiraItem(JiraIssue{Key: "OP-2", StatusCat: "done", Assignee: "bob"}),
			},
			currentUser: "alice",
			want: EpicStats{
				Total:      2,
				Open:       0,
				Done:       2,
				Blocked:    0,
				Unassigned: 0,
				MyCount:    1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeEpicStats(tt.items, tt.currentUser)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEpicStats_PopulatedOnDataLoaded(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Set current user (normally comes from jiraDataLoadedMsg).
	m.currentUser = "alice"

	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-1", Name: "Epic 1"},
			{EpicKey: "OP-2", Name: "Epic 2"},
		},
		items: [][]JiraItem{
			{
				NewJiraItem(JiraIssue{Key: "OP-10", StatusCat: "done", Assignee: "alice"}),
				NewJiraItem(JiraIssue{Key: "OP-11", StatusCat: "new", Assignee: "bob"}),
				NewJiraItem(JiraIssue{Key: "OP-12", StatusCat: "new", Assignee: ""}),
			},
			{
				NewJiraItem(JiraIssue{Key: "OP-20", StatusCat: "done", Assignee: "alice"}),
			},
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	require.Len(t, m.epicStats, 2)

	// Section 0: 3 items, 1 done, 2 open, 1 unassigned, 1 mine.
	assert.Equal(t, 3, m.epicStats[0].Total)
	assert.Equal(t, 1, m.epicStats[0].Done)
	assert.Equal(t, 2, m.epicStats[0].Open)
	assert.Equal(t, 1, m.epicStats[0].Unassigned)
	assert.Equal(t, 1, m.epicStats[0].MyCount)

	// Section 1: 1 item, 1 done, 0 open, 1 mine.
	assert.Equal(t, 1, m.epicStats[1].Total)
	assert.Equal(t, 1, m.epicStats[1].Done)
	assert.Equal(t, 0, m.epicStats[1].Open)
	assert.Equal(t, 1, m.epicStats[1].MyCount)
}

func TestRenderProgressBar(t *testing.T) {
	// 0 total: all empty.
	bar := renderProgressBar(0, 0, 10)
	assert.Equal(t, 10, countRune(bar, '░'))

	// 100%: all filled.
	bar = renderProgressBar(5, 5, 10)
	assert.Equal(t, 10, countRune(bar, '█'))

	// 50%: half and half.
	bar = renderProgressBar(5, 10, 10)
	assert.Equal(t, 5, countRune(bar, '█'))
	assert.Equal(t, 5, countRune(bar, '░'))
}

// countRune counts occurrences of a rune in a string (ignoring ANSI escape codes).
func countRune(s string, r rune) int {
	// Strip ANSI escape sequences for counting.
	cleaned := stripAnsi(s)
	count := 0
	for _, c := range cleaned {
		if c == r {
			count++
		}
	}
	return count
}

// stripAnsi removes ANSI escape sequences from a string.
func stripAnsi(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestRenderEpicSectionHeader_ContainsStats(t *testing.T) {
	sec := EpicSection{EpicKey: "OP-3309", Name: "UX/UI Design"}
	stats := EpicStats{Total: 20, Open: 7, Done: 13, Blocked: 1, Unassigned: 2, MyCount: 3}

	header := renderEpicSectionHeader(sec, stats, true, false)
	stripped := stripAnsi(header)

	assert.Contains(t, stripped, "OP-3309")
	assert.Contains(t, stripped, "UX/UI Design")
	assert.Contains(t, stripped, "65%")
	assert.Contains(t, stripped, "13/20 done")
	assert.Contains(t, stripped, "3 mine")
	assert.Contains(t, stripped, "1 blocked")
	assert.Contains(t, stripped, "2 unassigned")
}

func TestRenderEpicSectionHeader_CollapsedStillShowsStats(t *testing.T) {
	sec := EpicSection{EpicKey: "OP-1", Name: "Test"}
	stats := EpicStats{Total: 5, Done: 3}

	header := renderEpicSectionHeader(sec, stats, false, true)
	stripped := stripAnsi(header)

	// Collapsed indicator.
	assert.Contains(t, stripped, ">")
	// Stats still present.
	assert.Contains(t, stripped, "60%")
	assert.Contains(t, stripped, "3/5 done")
}

func TestRenderEpicSectionHeader_EmptyEpic(t *testing.T) {
	sec := EpicSection{EpicKey: "OP-1", Name: "Empty"}
	stats := EpicStats{}

	header := renderEpicSectionHeader(sec, stats, true, false)
	stripped := stripAnsi(header)

	assert.Contains(t, stripped, "(0)")
	// Should not contain progress bar fragments.
	assert.NotContains(t, stripped, "done")
}

// mockEpicManager implements EpicManager for tests.
type mockEpicManager struct {
	addedKey     string
	addedName    string
	toggledKey   string
	toggledState bool
	removedKey   string
}

func (m *mockEpicManager) AddTrackedEpic(_ context.Context, key, name string) error {
	m.addedKey = key
	m.addedName = name
	return nil
}

func (m *mockEpicManager) SetEpicActive(_ context.Context, key string, active bool) error {
	m.toggledKey = key
	m.toggledState = active
	return nil
}

func (m *mockEpicManager) RemoveTrackedEpic(_ context.Context, key string) error {
	m.removedKey = key
	return nil
}

func TestEpicAddPrompt_ActivatesOnA(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.activeTab = 4

	assert.False(t, m.epicAddActive)

	// Press 'a'.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)

	assert.True(t, m.epicAddActive)
}

func TestEpicAddPrompt_EscCancels(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.activeTab = 4
	m.epicAddActive = true

	// Press Esc.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	assert.False(t, m.epicAddActive)
}

func TestEpicAddPrompt_EnterSubmits(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.activeTab = 4
	m.epicAddActive = true
	m.epicAddInput.SetValue("OP-9999")

	// Press Enter.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	assert.False(t, m.epicAddActive)
	assert.NotNil(t, cmd, "should return an addEpic command")
	assert.Contains(t, m.statusText, "Adding OP-9999")
}

func TestEpicAddPrompt_EmptyInputIgnored(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.activeTab = 4
	m.epicAddActive = true
	m.epicAddInput.SetValue("   ")

	// Press Enter with empty/whitespace-only input.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	assert.False(t, m.epicAddActive)
	assert.Nil(t, cmd, "should return nil for empty input")
}

func TestEpicToggle_HidesEpic(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Load epic data.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-1", Name: "Epic 1"},
		},
		items: [][]JiraItem{
			{NewJiraItem(JiraIssue{Key: "OP-10", Summary: "Issue", Status: "To Do"})},
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Press 't' to toggle.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)

	assert.NotNil(t, cmd)
	assert.Contains(t, m.statusText, "Hiding OP-1")
}

func TestEpicRemove_RemovesEpic(t *testing.T) {
	mgr := &mockEpicManager{}
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(),
		WithEpicManager(mgr),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Load epic data.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{
			{EpicKey: "OP-1", Name: "Epic 1"},
		},
		items: [][]JiraItem{
			{NewJiraItem(JiraIssue{Key: "OP-10", Summary: "Issue", Status: "To Do"})},
		},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Press 'D' to remove.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated.(Model)

	assert.NotNil(t, cmd)
	assert.Contains(t, m.statusText, "Removing OP-1")
}

func TestEpicAddedMsg_ReloadsData(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	updated, cmd := m.Update(epicAddedMsg{key: "OP-5000"})
	m = updated.(Model)

	assert.Contains(t, m.statusText, "Added OP-5000")
	assert.NotNil(t, cmd, "should batch reload + clear status")
}

func TestEpicRemovedMsg_ClampsSection(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	// Simulate 2 sections, focused on section 1.
	m.epicSections = []EpicSection{
		{EpicKey: "OP-1", Name: "A"},
		{EpicKey: "OP-2", Name: "B"},
	}
	m.epicSection = 1

	// Remove triggers section clamp since we're at the last section.
	updated, _ = m.Update(epicRemovedMsg{key: "OP-2"})
	m = updated.(Model)

	assert.Equal(t, 0, m.epicSection, "should clamp to 0 after removing last section")
}

func TestPartitionEpicItems(t *testing.T) {
	items := []JiraItem{
		NewJiraItem(JiraIssue{Key: "OP-1", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-2", Assignee: "bob"}),
		NewJiraItem(JiraIssue{Key: "OP-3", Assignee: ""}),
		NewJiraItem(JiraIssue{Key: "OP-4", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-5", Assignee: "charlie"}),
		NewJiraItem(JiraIssue{Key: "OP-6", Assignee: ""}),
	}

	p := partitionEpicItems(items, "alice")

	assert.Len(t, p.mine, 2, "alice has 2 items")
	assert.Equal(t, "OP-1", p.mine[0].issue.Key)
	assert.Equal(t, "OP-4", p.mine[1].issue.Key)

	assert.Len(t, p.unassigned, 2, "2 unassigned items")
	assert.Equal(t, "OP-3", p.unassigned[0].issue.Key)
	assert.Equal(t, "OP-6", p.unassigned[1].issue.Key)

	assert.Len(t, p.others, 2, "bob and charlie")
	assert.Equal(t, "OP-2", p.others[0].issue.Key)
	assert.Equal(t, "OP-5", p.others[1].issue.Key)
}

func TestPartitionEpicItems_NoCurrentUser(t *testing.T) {
	items := []JiraItem{
		NewJiraItem(JiraIssue{Key: "OP-1", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-2", Assignee: ""}),
	}

	p := partitionEpicItems(items, "")

	assert.Len(t, p.mine, 0, "no current user means no mine")
	assert.Len(t, p.unassigned, 1)
	assert.Len(t, p.others, 1, "alice goes to others when no current user")
}

func TestEpicPartition_AllItems(t *testing.T) {
	p := epicPartition{
		mine:       []JiraItem{NewJiraItem(JiraIssue{Key: "OP-1"})},
		unassigned: []JiraItem{NewJiraItem(JiraIssue{Key: "OP-2"})},
		others:     []JiraItem{NewJiraItem(JiraIssue{Key: "OP-3"})},
	}

	all := p.allItems()
	assert.Len(t, all, 3)
	assert.Equal(t, "OP-1", all[0].issue.Key)
	assert.Equal(t, "OP-2", all[1].issue.Key)
	assert.Equal(t, "OP-3", all[2].issue.Key)
}

func TestEpicPartition_VisibleItems(t *testing.T) {
	p := epicPartition{
		mine:       []JiraItem{NewJiraItem(JiraIssue{Key: "OP-1"})},
		unassigned: []JiraItem{NewJiraItem(JiraIssue{Key: "OP-2"})},
		others:     []JiraItem{NewJiraItem(JiraIssue{Key: "OP-3"})},
	}

	visible := p.visibleItems()
	assert.Len(t, visible, 2)
	assert.Equal(t, "OP-1", visible[0].issue.Key)
	assert.Equal(t, "OP-2", visible[1].issue.Key)
}

func TestEpicShowOthers_Toggle(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items: [][]JiraItem{{
			NewJiraItem(JiraIssue{Key: "OP-10", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-11", Assignee: ""}),
			NewJiraItem(JiraIssue{Key: "OP-12", Assignee: "bob"}),
			NewJiraItem(JiraIssue{Key: "OP-13", Assignee: "charlie"}),
		}},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Default: others hidden, list shows mine + unassigned + group headers.
	require.Len(t, m.epicPartitions, 1)
	assert.Len(t, m.epicPartitions[0].mine, 1)
	assert.Len(t, m.epicPartitions[0].unassigned, 1)
	assert.Len(t, m.epicPartitions[0].others, 2)
	// 1 mine item + 1 unassigned item + 2 group headers = 4 list items.
	assert.Equal(t, 4, len(m.epicLists[0].Items()), "mine(1) + unassigned(1) + 2 headers")
	assert.False(t, m.epicShowOthers[0])

	// Press 'e' to show others.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)

	assert.True(t, m.epicShowOthers[0])
	// 4 real items + 3 group headers = 7 list items.
	assert.Equal(t, 7, len(m.epicLists[0].Items()), "all 4 items + 3 headers")

	// Press 'e' again to hide others.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)

	assert.False(t, m.epicShowOthers[0])
	assert.Equal(t, 4, len(m.epicLists[0].Items()), "back to mine + unassigned + 2 headers")
}

func TestEpicShowOthers_PreservedOnRefresh(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	items := [][]JiraItem{{
		NewJiraItem(JiraIssue{Key: "OP-10", Assignee: "alice"}),
		NewJiraItem(JiraIssue{Key: "OP-12", Assignee: "bob"}),
	}}

	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items:    items,
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Enable show others.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	assert.True(t, m.epicShowOthers[0])

	// Simulate data refresh — showOthers should be preserved.
	updated, _ = m.Update(epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items:    items,
	})
	m = updated.(Model)

	assert.True(t, m.epicShowOthers[0], "showOthers state should survive data refresh")
}

func TestEpicGroupHeaders_InsertedBetweenGroups(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items: [][]JiraItem{{
			NewJiraItem(JiraIssue{Key: "OP-10", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-11", Assignee: ""}),
		}},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	items := m.epicLists[0].Items()
	// Should be: Mine header, OP-10, Unassigned header, OP-11.
	require.Equal(t, 4, len(items))

	// First item should be a group header.
	_, isHeader := items[0].(epicGroupHeader)
	assert.True(t, isHeader, "first item should be Mine group header")

	// Second item should be a JiraItem.
	ji, isJira := items[1].(JiraItem)
	assert.True(t, isJira)
	assert.Equal(t, "OP-10", ji.issue.Key)

	// Third item should be another group header.
	_, isHeader = items[2].(epicGroupHeader)
	assert.True(t, isHeader, "third item should be Unassigned group header")

	// Fourth item should be a JiraItem.
	ji, isJira = items[3].(JiraItem)
	assert.True(t, isJira)
	assert.Equal(t, "OP-11", ji.issue.Key)
}

func TestEpicGroupHeaders_EmptyGroupOmitted(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	// All items are assigned to alice — no unassigned or others.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items: [][]JiraItem{{
			NewJiraItem(JiraIssue{Key: "OP-10", Assignee: "alice"}),
			NewJiraItem(JiraIssue{Key: "OP-11", Assignee: "alice"}),
		}},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	items := m.epicLists[0].Items()
	// Should be: Mine header + 2 items = 3 total (no Unassigned header).
	assert.Equal(t, 3, len(items))
}

func TestEpicSelectedItem_SkipsGroupHeaders(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.currentUser = "alice"

	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items: [][]JiraItem{{
			NewJiraItem(JiraIssue{Key: "OP-10", Assignee: "alice"}),
		}},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Index 0 is the group header — SelectedEpicItem should return false.
	m.epicLists[0].Select(0)
	_, ok := m.SelectedEpicItem()
	assert.False(t, ok, "group header should not be selectable as a JiraItem")

	// Index 1 is the actual item.
	m.epicLists[0].Select(1)
	item, ok := m.SelectedEpicItem()
	assert.True(t, ok)
	assert.Equal(t, "OP-10", item.issue.Key)
}

func TestEpicNoManagerKeys_Ignored(t *testing.T) {
	// Without EpicManager, a/t/D should not activate anything.
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	m.activeTab = 4

	// Load some epic data so the keybinding block is entered.
	msg := epicDataLoadedMsg{
		sections: []EpicSection{{EpicKey: "OP-1", Name: "Epic"}},
		items:    [][]JiraItem{{NewJiraItem(JiraIssue{Key: "OP-10", Summary: "X", Status: "To Do"})}},
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	m.activeTab = 4

	// Press 'a' — should not activate add prompt.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	assert.False(t, m.epicAddActive)

	// Press 't' — should not change status.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	assert.Equal(t, "", m.statusText)
}
