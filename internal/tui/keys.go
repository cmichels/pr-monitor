package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMap defines all TUI keybindings.
type keyMap struct {
	SwitchTab      key.Binding
	Review         key.Binding
	Dismiss        key.Binding
	OpenBrowser    key.Binding
	Refresh        key.Binding
	Help           key.Binding
	Quit           key.Binding
	DetailDown     key.Binding
	DetailUp       key.Binding
	FocusDetail    key.Binding
	FocusList      key.Binding
	SectionSwitch  key.Binding
	CollapseToggle key.Binding
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
		DetailDown: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "scroll detail down"),
		),
		DetailUp: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "scroll detail up"),
		),
		FocusDetail: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "focus detail panel"),
		),
		FocusList: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "focus list panel"),
		),
		SectionSwitch: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "switch section"),
		),
		CollapseToggle: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "collapse/expand"),
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

// launchReview opens a new wezterm tab in the repo directory with claude-code review.
func (m *Model) launchReview(pr PRItem) tea.Cmd {
	repo := pr.pr.Repo
	number := pr.pr.Number
	resolver := m.repoResolver

	return func() tea.Msg {
		path, found := resolver.Resolve(repo)
		if !found {
			return statusMsg{text: fmt.Sprintf("Repo not found locally: %s", repo)}
		}

		// Extract short repo name (e.g. "stg-devops-compose" from "Stark-Tech-Group/stg-devops-compose")
		repoShort := repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			repoShort = repo[idx+1:]
		}
		tabTitle := fmt.Sprintf("%s #%d", repoShort, number)

		// 1. Spawn a new tab with an interactive login shell in the repo dir
		spawn := exec.Command(
			"wezterm", "cli", "spawn",
			"--cwd", path,
		)
		out, err := spawn.Output()
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Failed to open tab: %v", err)}
		}
		paneID := strings.TrimSpace(string(out))

		// 2. Set the tab title
		if paneID != "" {
			setTitle := exec.Command("wezterm", "cli", "set-tab-title", "--pane-id", paneID, tabTitle)
			_ = setTitle.Run()
		}

		// 3. Pre-fill the claude review command in the pane (user presses Enter to run)
		if paneID != "" {
			reviewCmd := fmt.Sprintf("claude '/review-pr %d'", number)
			sendText := exec.Command("wezterm", "cli", "send-text", "--pane-id", paneID, reviewCmd)
			_ = sendText.Run()
		}

		return statusMsg{text: fmt.Sprintf("Reviewing %s #%d", repoShort, number)}
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
