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
	assert.NotEmpty(t, km.DetailDown.Keys(), "DetailDown keys")
	assert.NotEmpty(t, km.DetailUp.Keys(), "DetailUp keys")
	assert.NotEmpty(t, km.FocusDetail.Keys(), "FocusDetail keys")
	assert.NotEmpty(t, km.FocusList.Keys(), "FocusList keys")
	assert.NotEmpty(t, km.SectionSwitch.Keys(), "SectionSwitch keys")
	assert.NotEmpty(t, km.CollapseToggle.Keys(), "CollapseToggle keys")
	assert.NotEmpty(t, km.Undismiss.Keys(), "Undismiss keys")
	assert.NotEmpty(t, km.Claim.Keys(), "Claim keys")
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
				URL:            "https://github.com/org/repo-a/pull/42",
				FilesChanged:   3, CIStatus: "passing",
				ReviewerStatus: "pending",
				FirstSeen:      time.Now().Add(-1 * time.Hour),
			},
		},
		authorPRs: []PR{
			{
				PRID: "PR_author_1", Repo: "org/repo-b", Number: 99,
				Title: "My feature", Author: "me",
				URL:          "https://github.com/org/repo-b/pull/99",
				FilesChanged: 5, CIStatus: "passing",
				FirstSeen:    time.Now().Add(-2 * time.Hour),
			},
		},
	}
	m := New(loader, &mockResolver{}, DefaultShameConfig(), opts...)

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
	assert.Contains(t, view, "section")
	assert.Contains(t, view, "Collapse/expand")
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

	// Press ctrl+r to force refresh.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
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

func TestQuickReviewKey(t *testing.T) {
	m := setupModelWithItems(t)

	// Press 'r' to launch quick review.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.NotNil(t, cmd, "quick review should return a command")
}

func TestTeamReviewKey(t *testing.T) {
	m := setupModelWithItems(t)

	// Press 'R' to launch full team review.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	assert.NotNil(t, cmd, "team review should return a command")
}

func TestQuickReviewKey_RepoNotFound(t *testing.T) {
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
	m := New(loader, resolver, DefaultShameConfig())
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
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	footer := renderFooter(m)
	assert.Contains(t, footer, "review")
	assert.Contains(t, footer, "dismiss")
	assert.Contains(t, footer, "help")
	assert.Contains(t, footer, "quit")
}

func TestFooterContainsSectionHints_OnTab0(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	footer := renderFooter(m)
	assert.Contains(t, footer, "s:section")
	assert.Contains(t, footer, "x:fold")

	// Switch to tab 1 — section hints should also appear (Active/Drafts).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	footer = renderFooter(m)
	assert.Contains(t, footer, "s:section")
	assert.Contains(t, footer, "x:fold")
}

func TestFooterContainsScrollHints_WithDetailFetcher(t *testing.T) {
	m := New(&mockLoader{}, &mockResolver{}, DefaultShameConfig(), WithDetailFetcher(&mockDetailFetcher{}))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	footer := renderFooter(m)
	assert.Contains(t, footer, "ctrl+d/u")
}

func TestFocusDetail_WithFetcher(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	assert.False(t, m.detailFocused)

	// Press 'l' to focus detail panel.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	assert.True(t, m.detailFocused)

	// Press 'h' to return to list.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)
	assert.False(t, m.detailFocused)
}

func TestFocusDetail_EscReturnsToList(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	// Focus detail, then Esc back.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	assert.True(t, m.detailFocused)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	assert.False(t, m.detailFocused)
}

func TestFocusDetail_JKScrollsViewport(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	// Populate detail so viewport has content.
	m.activeDetail = &PRDetail{Body: "line1\nline2\nline3\nline4\nline5"}
	m.updateDetailViewport()

	// Focus detail.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)

	// j/k should not panic and should return without error.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	assert.True(t, m.detailFocused, "should remain focused after j")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	assert.True(t, m.detailFocused, "should remain focused after k")
}

func TestFocusDetail_TabUnfocuses(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	// Focus detail.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	assert.True(t, m.detailFocused)

	// Tab should switch tabs AND unfocus detail.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	assert.False(t, m.detailFocused, "tab should unfocus detail")
}

func TestFocusDetail_QuitStillWorks(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	// Focus detail.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)

	// 'q' should still quit even when detail is focused.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd, "quit should work when detail is focused")
}

func TestFocusDetail_NoFetcher(t *testing.T) {
	// Without a detail fetcher, 'l' should do nothing.
	m := setupModelWithItems(t)
	assert.False(t, m.detailFocused)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	assert.False(t, m.detailFocused, "should not focus when no detail fetcher")
}

func TestFooterChangesWithFocus(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)

	// List focused footer.
	footer := renderFooter(m)
	assert.Contains(t, footer, "l:detail")

	// Detail focused footer.
	m.detailFocused = true
	footer = renderFooter(m)
	assert.Contains(t, footer, "j/k:scroll")
	assert.Contains(t, footer, "h:back to list")
}

func TestExpandCollapseComments(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)
	assert.False(t, m.commentsExpanded)

	// Focus detail.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)

	// Press 'e' to expand.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	assert.True(t, m.commentsExpanded)

	// Press 'e' again to collapse.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	assert.False(t, m.commentsExpanded)
}

func TestFooterShowsExpandHint(t *testing.T) {
	fetcher := &mockDetailFetcher{result: &PRDetail{Body: "test"}}
	m := setupModelWithDetail(t, fetcher)
	m.detailFocused = true

	footer := renderFooter(m)
	assert.Contains(t, footer, "e:expand")

	m.commentsExpanded = true
	footer = renderFooter(m)
	assert.Contains(t, footer, "e:collapse")
}

// mockResolverNotFound always reports repos as not found.
type mockResolverNotFound struct{}

func (r *mockResolverNotFound) Resolve(repo string) (string, bool) {
	return "", false
}
