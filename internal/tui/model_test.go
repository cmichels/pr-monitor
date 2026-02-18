package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

// mockLoader implements PRLoader for tests.
type mockLoader struct {
	reviewPRs []PR
	authorPRs []PR
	err       error
	callCount int
}

func (m *mockLoader) GetPendingByRole(_ context.Context, role string) ([]PR, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if role == "reviewer" {
		return m.reviewPRs, nil
	}
	return m.authorPRs, nil
}

// mockResolver implements RepoResolver for tests.
type mockResolver struct{}

func (m *mockResolver) Resolve(repo string) (string, bool) {
	return "/fake/path/" + repo, true
}

func samplePR(repo string, number int, title string, age time.Duration) PR {
	return PR{
		PRID:         "PR_" + repo + "_" + title,
		Repo:         repo,
		Number:       number,
		Title:        title,
		Author:       "testuser",
		URL:          "https://github.com/" + repo + "/pull/" + title,
		FilesChanged: 3,
		CIStatus:     "passing",
		FirstSeen:    time.Now().Add(-age),
		Status:       "pending",
	}
}

func TestNew_InitialState(t *testing.T) {
	loader := &mockLoader{}
	resolver := &mockResolver{}
	m := New(loader, resolver, DefaultShameConfig())

	assert.Equal(t, 0, m.activeTab, "should start on first tab")
	assert.Len(t, m.tabs, 2, "should have two tabs")
	assert.Equal(t, "To Review", m.tabs[0])
	assert.Equal(t, "My PRs", m.tabs[1])
	assert.Len(t, m.lists, 2, "should have two list models")
	assert.Nil(t, m.err, "should have no initial error")
}

func TestTabSwitching(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	// Simulate WindowSizeMsg so the model is ready.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	assert.Equal(t, 0, m.activeTab)

	// Tab forward.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	assert.Equal(t, 1, m.activeTab)

	// Tab forward again wraps around.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	assert.Equal(t, 0, m.activeTab)

	// Shift+tab goes backward (wraps to last tab).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	assert.Equal(t, 1, m.activeTab)
}

func TestPrsLoadedMsg_PopulatesLists(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	// Need a window size first.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	shame := DefaultShameConfig()
	reviewItems := []PRItem{
		NewPRItem(samplePR("org/repo-a", 1, "Fix bug", 2*time.Hour), shame),
		NewPRItem(samplePR("org/repo-b", 2, "Add feature", 1*time.Hour), shame),
	}
	authorItems := []PRItem{
		NewPRItem(samplePR("org/repo-c", 3, "My PR", 30*time.Minute), shame),
	}

	updated, _ = m.Update(prsLoadedMsg{
		reviewPRs:   reviewItems,
		authoredPRs: authorItems,
	})
	m = updated.(Model)

	assert.Len(t, m.lists[0].Items(), 2, "review tab should have 2 items")
	assert.Len(t, m.lists[1].Items(), 1, "author tab should have 1 item")
	assert.Nil(t, m.err)
}

func TestPrsLoadedMsg_Error(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(prsLoadedMsg{
		err: assert.AnError,
	})
	m = updated.(Model)

	assert.Error(t, m.err)
}

func TestRefreshMsg_TriggersDataLoad(t *testing.T) {
	loader := &mockLoader{
		reviewPRs: []PR{samplePR("org/repo", 1, "Test", time.Hour)},
	}
	m := New(loader, &mockResolver{}, DefaultShameConfig())

	updated, cmd := m.Update(RefreshMsg{})
	m = updated.(Model)

	assert.NotNil(t, cmd, "RefreshMsg should return a loadData command")

	// Execute the command and verify it calls the loader.
	msg := cmd()
	loaded, ok := msg.(prsLoadedMsg)
	assert.True(t, ok, "command should return prsLoadedMsg")
	assert.NoError(t, loaded.err)
	assert.Len(t, loaded.reviewPRs, 1)
}

func TestWindowSizeMsg_UpdatesDimensions(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}

func TestPRItem_Title(t *testing.T) {
	pr := PR{
		Repo:   "myorg/myrepo",
		Number: 42,
		Title:  "Add dark mode",
	}
	item := NewPRItem(pr, DefaultShameConfig())
	assert.Equal(t, "myorg/myrepo #42  Add dark mode", item.Title())
}

func TestPRItem_Description(t *testing.T) {
	pr := PR{
		Author:       "alice",
		FilesChanged: 5,
		CIStatus:     "passing",
		FirstSeen:    time.Now().Add(-3 * time.Hour),
	}
	item := NewPRItem(pr, DefaultShameConfig())
	desc := item.Description()
	assert.Contains(t, desc, "@alice")
	assert.Contains(t, desc, "5 files")
	assert.Contains(t, desc, "ok")
	assert.Contains(t, desc, "3h")
}

func TestPRItem_DescriptionWithActivity(t *testing.T) {
	pr := PR{
		Author:           "alice",
		FilesChanged:     2,
		CIStatus:         "failing",
		FirstSeen:        time.Now().Add(-1 * time.Hour),
		LastActivityType: "approved",
		LastActivityBy:   "bob",
	}
	item := NewPRItem(pr, DefaultShameConfig())
	desc := item.Description()
	assert.Contains(t, desc, "FAIL")
	assert.Contains(t, desc, "approved")
	assert.Contains(t, desc, "@bob")
}

func TestPRItem_FilterValue(t *testing.T) {
	pr := PR{Title: "My Cool PR"}
	item := NewPRItem(pr, DefaultShameConfig())
	assert.Equal(t, "My Cool PR", item.FilterValue())
}

func TestView_BeforeWindowSize(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	view := m.View()
	assert.Equal(t, "Initializing...", view)
}

func TestLoadData_SortsAuthoredByActivity(t *testing.T) {
	now := time.Now()
	loader := &mockLoader{
		authorPRs: []PR{
			{
				PRID: "PR1", Repo: "org/a", Number: 1, Title: "Older",
				Author: "me", FirstSeen: now.Add(-2 * time.Hour),
				LastActivityAt: now.Add(-2 * time.Hour),
			},
			{
				PRID: "PR2", Repo: "org/b", Number: 2, Title: "Newer",
				Author: "me", FirstSeen: now.Add(-1 * time.Hour),
				LastActivityAt: now.Add(-10 * time.Minute),
			},
		},
	}
	m := New(loader, &mockResolver{}, DefaultShameConfig())
	cmd := m.loadData()
	msg := cmd().(prsLoadedMsg)

	assert.Len(t, msg.authoredPRs, 2)
	// Newer activity should come first.
	assert.Equal(t, "Newer", msg.authoredPRs[0].pr.Title)
	assert.Equal(t, "Older", msg.authoredPRs[1].pr.Title)
}

func TestPollErrorMsg_ShowsAndAutoDismisses(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Send a poll error message.
	updated, cmd := m.Update(PollErrorMsg{Text: "GitHub API unreachable -- retrying..."})
	m = updated.(Model)

	assert.Equal(t, "GitHub API unreachable -- retrying...", m.errorText)
	assert.NotNil(t, cmd, "should return a tick command for auto-dismiss")

	// Any keypress should clear the error.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	assert.Empty(t, m.errorText, "keypress should dismiss error")
}

func TestClearErrorMsg_DismissesError(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	m.errorText = "some error"

	updated, _ := m.Update(clearErrorMsg{})
	m = updated.(Model)

	assert.Empty(t, m.errorText)
}

func TestPollErrorMsg_RendersInView(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(PollErrorMsg{Text: "Rate limited -- next poll in 30s"})
	m = updated.(Model)

	view := m.View()
	assert.Contains(t, view, "Rate limited -- next poll in 30s")
}
