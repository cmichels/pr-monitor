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
	CopyURL        key.Binding
	Refresh        key.Binding
	Help           key.Binding
	Quit           key.Binding
	DetailDown     key.Binding
	DetailUp       key.Binding
	FocusDetail    key.Binding
	FocusList      key.Binding
	SectionSwitch  key.Binding
	CollapseToggle key.Binding
	Claim          key.Binding
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
		CopyURL: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy URL"),
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
		Claim: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "claim issue"),
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

		// 3. Wait for shell to initialize, then pre-fill the claude review command
		if paneID != "" {
			time.Sleep(1500 * time.Millisecond)
			reviewCmd := fmt.Sprintf("claude --model claude-sonnet-4-6 '/review-pr %d'", number)
			sendText := exec.Command("wezterm", "cli", "send-text", "--no-paste", "--pane-id", paneID, reviewCmd)
			_ = sendText.Run()
		}

		return statusMsg{text: fmt.Sprintf("Reviewing %s #%d", repoShort, number)}
	}
}

// addressComments opens a new wezterm tab with claude to walk through review comments.
func (m *Model) addressComments(pr PRItem) tea.Cmd {
	repo := pr.pr.Repo
	number := pr.pr.Number
	resolver := m.repoResolver

	return func() tea.Msg {
		path, found := resolver.Resolve(repo)
		if !found {
			return statusMsg{text: fmt.Sprintf("Repo not found locally: %s", repo)}
		}

		repoShort := repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			repoShort = repo[idx+1:]
		}
		tabTitle := fmt.Sprintf("%s #%d addr", repoShort, number)

		// 1. Spawn a new tab with an interactive login shell in the repo dir
		spawn := exec.Command("wezterm", "cli", "spawn", "--cwd", path)
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

		// 3. Wait for shell to initialize, then send claude prompt
		if paneID != "" {
			time.Sleep(1500 * time.Millisecond)

			prompt := fmt.Sprintf(
				`claude "Address review feedback on PR #%d in %s. `+
					`Fetch comments with: gh pr view %d --json reviews,comments `+
					`and gh api 'repos/%s/pulls/%d/comments'. `+
					`Filter out bot comments and dismissed reviews. `+
					`Group remaining feedback by file then reviewer. `+
					`For each actionable comment show: reviewer name, file/line, the feedback, and a code snippet for context. `+
					`Ask me how to address each one and wait for my response before making changes. `+
					`After I respond, implement the change then move to the next comment. Start now."`,
				number, repo, number, repo, number,
			)

			sendText := exec.Command("wezterm", "cli", "send-text", "--no-paste", "--pane-id", paneID, prompt)
			_ = sendText.Run()
		}

		return statusMsg{text: fmt.Sprintf("Addressing comments on %s #%d", repoShort, number)}
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

// copyURL copies a URL to the system clipboard (macOS pbcopy).
func copyURL(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(url)
		if err := cmd.Run(); err != nil {
			return statusMsg{text: fmt.Sprintf("Copy failed: %v", err)}
		}
		return statusMsg{text: "URL copied to clipboard"}
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
