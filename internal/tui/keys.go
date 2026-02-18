package tui

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMap defines all TUI keybindings.
type keyMap struct {
	SwitchTab   key.Binding
	Review      key.Binding
	Dismiss     key.Binding
	OpenBrowser key.Binding
	Refresh     key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		SwitchTab: key.NewBinding(
			key.WithKeys("tab", "shift+tab"),
			key.WithHelp("tab/shift+tab", "switch tab"),
		),
		Review: key.NewBinding(
			key.WithKeys("r", "enter"),
			key.WithHelp("r/enter", "launch review"),
		),
		Dismiss: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "dismiss"),
		),
		OpenBrowser: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open in browser"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "force refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// Dismisser abstracts the store for dismissing PRs.
type Dismisser interface {
	Dismiss(ctx context.Context, prID string) error
}

// -- Messages -----------------------------------------------------------------

type dismissMsg struct{ prID string }
type dismissErrMsg struct{ err error }
type statusMsg struct{ text string }
type clearStatusMsg struct{}
type reviewLaunchedMsg struct{}
type browserOpenedMsg struct{}

// -- Commands -----------------------------------------------------------------

// launchReview opens a new wezterm pane in the repo directory with claude-code.
func (m *Model) launchReview(pr PRItem) tea.Cmd {
	repo := pr.pr.Repo
	number := pr.pr.Number
	resolver := m.repoResolver

	return func() tea.Msg {
		path, found := resolver.Resolve(repo)
		if !found {
			return statusMsg{text: fmt.Sprintf("Repo not found locally: %s", repo)}
		}

		cmd := exec.Command(
			"wezterm", "cli", "split-pane",
			"--cwd", path,
			"--", "claude", "-p", fmt.Sprintf("/review-pr %d", number),
		)
		if err := cmd.Start(); err != nil {
			return statusMsg{text: fmt.Sprintf("Failed to launch review: %v", err)}
		}
		return statusMsg{text: fmt.Sprintf("Launching review for %s #%d...", repo, number)}
	}
}

// dismissPR returns a Cmd that calls the Dismisser and emits a dismissMsg.
func (m *Model) dismissPR(pr PRItem) tea.Cmd {
	dismisser := m.dismisser
	prID := pr.pr.PRID

	return func() tea.Msg {
		if dismisser == nil {
			return statusMsg{text: "Dismiss not available"}
		}
		if err := dismisser.Dismiss(context.Background(), prID); err != nil {
			return dismissErrMsg{err: err}
		}
		return dismissMsg{prID: prID}
	}
}

// openBrowser opens a URL in the default browser (macOS).
func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("open", url)
		_ = cmd.Start()
		return statusMsg{text: "Opened in browser"}
	}
}

// clearStatusAfter returns a Cmd that sends a clearStatusMsg after a delay.
func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}
