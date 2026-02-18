package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestDefaultKeyMap_AllBindingsSet(t *testing.T) {
	km := defaultKeyMap()

	assert.NotEmpty(t, km.SwitchTab.Keys(), "SwitchTab keys")
	assert.NotEmpty(t, km.Review.Keys(), "Review keys")
	assert.NotEmpty(t, km.Dismiss.Keys(), "Dismiss keys")
	assert.NotEmpty(t, km.OpenBrowser.Keys(), "OpenBrowser keys")
	assert.NotEmpty(t, km.Refresh.Keys(), "Refresh keys")
	assert.NotEmpty(t, km.Help.Keys(), "Help keys")
	assert.NotEmpty(t, km.Quit.Keys(), "Quit keys")
}

func TestDefaultKeyMap_HelpText(t *testing.T) {
	km := defaultKeyMap()

	tests := []struct {
		name    string
		binding struct{ Key, Desc string }
	}{
		{"SwitchTab", struct{ Key, Desc string }{km.SwitchTab.Help().Key, km.SwitchTab.Help().Desc}},
		{"Review", struct{ Key, Desc string }{km.Review.Help().Key, km.Review.Help().Desc}},
		{"Dismiss", struct{ Key, Desc string }{km.Dismiss.Help().Key, km.Dismiss.Help().Desc}},
		{"OpenBrowser", struct{ Key, Desc string }{km.OpenBrowser.Help().Key, km.OpenBrowser.Help().Desc}},
		{"Refresh", struct{ Key, Desc string }{km.Refresh.Help().Key, km.Refresh.Help().Desc}},
		{"Help", struct{ Key, Desc string }{km.Help.Help().Key, km.Help.Help().Desc}},
		{"Quit", struct{ Key, Desc string }{km.Quit.Help().Key, km.Quit.Help().Desc}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.binding.Key, "help key text")
			assert.NotEmpty(t, tt.binding.Desc, "help description")
		})
	}
}

// mockDismisser implements Dismisser for tests.
type mockDismisser struct {
	dismissed []string
	err       error
}

func (d *mockDismisser) Dismiss(_ context.Context, prID string) error {
	if d.err != nil {
		return d.err
	}
	d.dismissed = append(d.dismissed, prID)
	return nil
}

// helper to create a model with items loaded and window sized.
func setupModelWithItems(t *testing.T, opts ...Option) Model {
	t.Helper()
	loader := &mockLoader{
		reviewPRs: []PR{
			{
				PRID: "PR_review_1", Repo: "org/repo-a", Number: 42,
				Title: "Fix bug", Author: "alice",
				URL: "https://github.com/org/repo-a/pull/42",
				FilesChanged: 3, CIStatus: "passing",
				FirstSeen: time.Now().Add(-1 * time.Hour),
			},
		},
		authorPRs: []PR{
			{
				PRID: "PR_author_1", Repo: "org/repo-b", Number: 99,
				Title: "My feature", Author: "me",
				URL: "https://github.com/org/repo-b/pull/99",
				FilesChanged: 5, CIStatus: "passing",
				FirstSeen: time.Now().Add(-2 * time.Hour),
			},
		},
	}
	m := New(loader, &mockResolver{}, opts...)

	// Size the window.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	// Load data.
	cmd := m.loadData()
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(Model)

	return m
}

func TestHelpToggle(t *testing.T) {
	m := setupModelWithItems(t)
	assert.False(t, m.showHelp)

	// Press '?' to show help.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	assert.True(t, m.showHelp)

	// Any key dismisses help.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	assert.False(t, m.showHelp)
}

func TestHelpOverlayInView(t *testing.T) {
	m := setupModelWithItems(t)

	// Show help.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)

	view := m.View()
	assert.Contains(t, view, "Keybindings")
	assert.Contains(t, view, "Switch tabs")
	assert.Contains(t, view, "Quit")
}

func TestQuitKey(t *testing.T) {
	m := setupModelWithItems(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	// tea.Quit returns a special quit message.
	assert.NotNil(t, cmd, "quit should return a command")
}

func TestDismissKey(t *testing.T) {
	dismisser := &mockDismisser{}
	m := setupModelWithItems(t, WithDismisser(dismisser))

	// Press 'd' to dismiss selected item.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.NotNil(t, cmd, "dismiss should return a command")

	// Execute the command.
	msg := cmd()
	dMsg, ok := msg.(dismissMsg)
	assert.True(t, ok, "should return dismissMsg")
	assert.Equal(t, "PR_review_1", dMsg.prID)
	assert.Equal(t, []string{"PR_review_1"}, dismisser.dismissed)
}

func TestDismissKey_Error(t *testing.T) {
	dismisser := &mockDismisser{err: errors.New("db error")}
	m := setupModelWithItems(t, WithDismisser(dismisser))

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	msg := cmd()

	errMsg, ok := msg.(dismissErrMsg)
	assert.True(t, ok, "should return dismissErrMsg on error")
	assert.Error(t, errMsg.err)
}

func TestDismissMsg_SetsStatus(t *testing.T) {
	m := setupModelWithItems(t)

	updated, cmd := m.Update(dismissMsg{prID: "PR_42"})
	m = updated.(Model)

	assert.Contains(t, m.statusText, "Dismissed PR PR_42")
	assert.NotNil(t, cmd, "should return batch cmd for reload + clear")
}

func TestDismissErrMsg_SetsStatus(t *testing.T) {
	m := setupModelWithItems(t)

	updated, cmd := m.Update(dismissErrMsg{err: errors.New("oops")})
	m = updated.(Model)

	assert.Contains(t, m.statusText, "Dismiss failed")
	assert.NotNil(t, cmd, "should return clear status cmd")
}

func TestStatusMsg_SetsAndClears(t *testing.T) {
	m := setupModelWithItems(t)

	// Set status.
	updated, cmd := m.Update(statusMsg{text: "Test message"})
	m = updated.(Model)
	assert.Equal(t, "Test message", m.statusText)
	assert.NotNil(t, cmd, "should schedule clear")

	// Clear status.
	updated, _ = m.Update(clearStatusMsg{})
	m = updated.(Model)
	assert.Empty(t, m.statusText)
}

func TestRefreshKey(t *testing.T) {
	m := setupModelWithItems(t)

	// Press 'R' (shift+r) to force refresh.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(Model)

	assert.Equal(t, "Refreshing...", m.statusText)
	assert.NotNil(t, cmd, "refresh should return a batch command")
}

func TestOpenBrowserKey(t *testing.T) {
	m := setupModelWithItems(t)

	// Press 'o' to open in browser.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	assert.NotNil(t, cmd, "open browser should return a command")
}

func TestReviewKey(t *testing.T) {
	m := setupModelWithItems(t)

	// Press 'r' to launch review.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.NotNil(t, cmd, "review should return a command")
}

func TestReviewKey_RepoNotFound(t *testing.T) {
	// Use a resolver that always returns not found.
	resolver := &mockResolverNotFound{}
	loader := &mockLoader{
		reviewPRs: []PR{
			{
				PRID: "PR_1", Repo: "org/missing", Number: 1,
				Title: "Test", Author: "alice",
				URL: "https://github.com/org/missing/pull/1",
			},
		},
	}
	m := New(loader, resolver)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	cmd := m.loadData()
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	_, reviewCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.NotNil(t, reviewCmd)

	msg := reviewCmd()
	sMsg, ok := msg.(statusMsg)
	assert.True(t, ok)
	assert.Contains(t, sMsg.text, "Repo not found locally")
}

func TestDismissKey_NoDismisser(t *testing.T) {
	// No WithDismisser option — dismisser is nil.
	m := setupModelWithItems(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.NotNil(t, cmd)

	msg := cmd()
	sMsg, ok := msg.(statusMsg)
	assert.True(t, ok, "should return statusMsg when dismisser is nil")
	assert.Contains(t, sMsg.text, "not available")
}

func TestStatusLineInView(t *testing.T) {
	m := setupModelWithItems(t)
	m.statusText = "Test status"

	view := m.View()
	assert.Contains(t, view, "Test status")
}

func TestFooterContainsKeyHints(t *testing.T) {
	footer := renderFooter()
	assert.Contains(t, footer, "review")
	assert.Contains(t, footer, "dismiss")
	assert.Contains(t, footer, "help")
	assert.Contains(t, footer, "quit")
}

// mockResolverNotFound always reports repos as not found.
type mockResolverNotFound struct{}

func (r *mockResolverNotFound) Resolve(repo string) (string, bool) {
	return "", false
}
