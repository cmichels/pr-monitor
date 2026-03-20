package tui

import (
	"context"
	"fmt"
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
	Undismiss      key.Binding
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
		Undismiss: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "restore dismissed"),
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

// Undismisser abstracts the store for restoring dismissed PRs.
type Undismisser interface {
	Undismiss(ctx context.Context, prID string) error
}

// -- Messages -----------------------------------------------------------------

type dismissMsg struct{ prID string }
type dismissErrMsg struct{ err error }
type undismissMsg struct{ prID string }
type undismissErrMsg struct{ err error }
type statusMsg struct{ text string }
type clearStatusMsg struct{}

// -- Commands -----------------------------------------------------------------

// launchReview opens a new tmux window in the repo directory with claude-code review.
func (m *Model) launchReview(pr PRItem) tea.Cmd {
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
		tabTitle := fmt.Sprintf("review #%d", number)

		driver, err := DetectDriver("auto")
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Launch failed: %v", err)}
		}

		windowID, err := driver.SpawnWindow(path)
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Failed to open window: %v", err)}
		}

		if windowID != "" {
			_ = driver.SetTitle(windowID, tabTitle)
			time.Sleep(200 * time.Millisecond)
			_ = driver.SendLine(windowID, fmt.Sprintf("git stash && gh pr checkout %d", number))
			time.Sleep(800 * time.Millisecond)
			reviewCmd := fmt.Sprintf("claude --model claude-sonnet-4-6 '/review-pr %d'", number)
			_ = driver.SendText(windowID, reviewCmd)
		}

		return statusMsg{text: fmt.Sprintf("Reviewing %s #%d", repoShort, number)}
	}
}

// addressComments opens a new tmux window with claude to walk through review comments.
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

		driver, err := DetectDriver("auto")
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Launch failed: %v", err)}
		}

		windowID, err := driver.SpawnWindow(path)
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Failed to open window: %v", err)}
		}

		if windowID != "" {
			_ = driver.SetTitle(windowID, tabTitle)
			time.Sleep(500 * time.Millisecond)

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
			_ = driver.SendText(windowID, prompt)
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

// undismissPR returns a Cmd that calls the Undismisser and emits an undismissMsg.
func (m *Model) undismissPR(pr PRItem) tea.Cmd {
	undismisser := m.undismisser
	prID := pr.pr.PRID

	return func() tea.Msg {
		if undismisser == nil {
			return statusMsg{text: "Restore not available"}
		}
		if err := undismisser.Undismiss(context.Background(), prID); err != nil {
			return undismissErrMsg{err: err}
		}
		return undismissMsg{prID: prID}
	}
}

// copyURL copies a URL to the system clipboard.
func copyURL(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := clipboardCmd()
		cmd.Stdin = strings.NewReader(url)
		if err := cmd.Run(); err != nil {
			return statusMsg{text: fmt.Sprintf("Copy failed: %v", err)}
		}
		return statusMsg{text: "URL copied to clipboard"}
	}
}

// openBrowser opens a URL in the default browser.
func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if err := browserCmd(url).Start(); err != nil {
			return statusMsg{text: fmt.Sprintf("Browser failed: %v", err)}
		}
		return statusMsg{text: "Opened in browser"}
	}
}

// clearStatusAfter returns a Cmd that sends a clearStatusMsg after a delay.
func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}
