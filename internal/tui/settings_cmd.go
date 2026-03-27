package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Rescanner is an optional capability for resolvers that support re-scanning
// workspace directories. The Settings tab type-asserts RepoResolver to this.
type Rescanner interface {
	Rescan() error
}

// settingsAction defines a selectable action in the Settings tab.
type settingsAction struct {
	id   string // stable identifier for matching
	name string
	desc string
}

// settingsActionRescan is the action ID for repo rescanning.
const settingsActionRescan = "rescan"

// buildSettingsActions returns the list of available actions based on configured loaders.
func buildSettingsActions(m *Model) []settingsAction {
	var actions []settingsAction

	if _, ok := m.repoResolver.(Rescanner); ok {
		actions = append(actions, settingsAction{
			id:   settingsActionRescan,
			name: "Rescan Repos",
			desc: "Re-discover local git repos from workspace_dirs",
		})
	}

	actions = append(actions, settingsAction{
		id:   "refresh_prs",
		name: "Force Refresh PRs",
		desc: "Fetch latest PR data from GitHub",
	})

	if m.jiraLoader != nil {
		actions = append(actions, settingsAction{
			id:   "refresh_jira",
			name: "Force Refresh Jira",
			desc: "Fetch latest Jira data",
		})
	}

	if m.epicLoader != nil {
		actions = append(actions, settingsAction{
			id:   "refresh_epics",
			name: "Force Refresh Epics",
			desc: "Reload epic issues",
		})
	}

	if m.sprintLoader != nil {
		actions = append(actions, settingsAction{
			id:   "refresh_sprint",
			name: "Force Refresh Sprint",
			desc: "Reload sprint issues",
		})
	}

	return actions
}

// rescanReposMsg is sent when repo rescan completes.
type rescanReposMsg struct {
	err error
}

// rescanRepos returns a Cmd that triggers a repo rescan.
func rescanRepos(resolver RepoResolver) tea.Cmd {
	return func() tea.Msg {
		r, ok := resolver.(Rescanner)
		if !ok {
			return rescanReposMsg{err: fmt.Errorf("resolver does not support rescan")}
		}
		return rescanReposMsg{err: r.Rescan()}
	}
}

// executeSettingsAction dispatches the selected settings action.
func (m *Model) executeSettingsAction() (Model, tea.Cmd) {
	if len(m.settingsActions) == 0 {
		return *m, nil
	}
	action := m.settingsActions[m.settingsCursor]

	switch action.id {
	case settingsActionRescan:
		m.statusText = "Rescanning repos..."
		return *m, tea.Batch(rescanRepos(m.repoResolver), clearStatusAfter(3*time.Second))

	case "refresh_prs":
		m.statusText = "Refreshing PRs..."
		m.prsGen++
		m.prsLoading = true
		return *m, tea.Batch(m.loadData(), m.spinner.Tick, clearStatusAfter(3*time.Second))

	case "refresh_jira":
		m.statusText = "Refreshing Jira..."
		m.jiraGen++
		m.jiraLoading = true
		cmds := []tea.Cmd{m.loadJiraData(), m.spinner.Tick, clearStatusAfter(3 * time.Second)}
		if m.statsReady {
			cmds = append(cmds, m.loadStatsData())
		}
		return *m, tea.Batch(cmds...)

	case "refresh_epics":
		m.statusText = "Refreshing Epics..."
		m.epicGen++
		m.epicLoading = true
		return *m, tea.Batch(m.loadEpicData(), m.spinner.Tick, clearStatusAfter(3*time.Second))

	case "refresh_sprint":
		m.statusText = "Refreshing Sprint..."
		m.sprintGen++
		m.sprintLoading = true
		return *m, tea.Batch(m.loadSprintData(), m.spinner.Tick, clearStatusAfter(3*time.Second))
	}

	return *m, nil
}
