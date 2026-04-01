package tui

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/chrismichels/pr-monitor/internal/store"
)

// tasksDataLoadedMsg is returned when task data finishes loading from the store.
type tasksDataLoadedMsg struct {
	tasks []store.DevTask
	gen   uint64
	err   error
}

// taskActionDoneMsg is returned after a task-ctl CLI action completes.
type taskActionDoneMsg struct {
	action string
	key    string
	err    error
}

func (m *Model) loadTasksData() tea.Cmd {
	gen := m.tasksGen
	loader := m.tasksLoader
	filter := ""
	if m.tasksShowAll {
		filter = "all"
	}
	return func() tea.Msg {
		if loader == nil {
			return tasksDataLoadedMsg{gen: gen}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tasks, err := loader.ListDevTasks(ctx, filter)
		return tasksDataLoadedMsg{tasks: tasks, gen: gen, err: err}
	}
}

// runTaskAction runs a task-ctl subcommand using the task's numeric ID
// (unambiguous even when multiple tasks share the same Jira key).
func runTaskAction(action string, taskID int, jiraKey string) tea.Cmd {
	idStr := fmt.Sprintf("%d", taskID)
	return func() tea.Msg {
		if _, err := exec.LookPath("task-ctl"); err != nil {
			return taskActionDoneMsg{
				action: action,
				key:    jiraKey,
				err:    fmt.Errorf("task-ctl not found in PATH — install it to manage tasks"),
			}
		}
		cmd := exec.Command("task-ctl", action, idStr)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return taskActionDoneMsg{
				action: action,
				key:    jiraKey,
				err:    fmt.Errorf("%s: %s", err, string(out)),
			}
		}
		return taskActionDoneMsg{action: action, key: jiraKey}
	}
}

// maybeLoadJiraDetailForTask triggers a live Jira detail fetch for the selected task.
// Reuses the existing jiraDetailFetcher and cache from the Jira tab.
func (m *Model) maybeLoadJiraDetailForTask() tea.Cmd {
	if m.jiraDetailFetcher == nil || !m.detailReady {
		return nil
	}
	if len(m.devTasks) == 0 || m.tasksCursor >= len(m.devTasks) {
		return nil
	}

	key := m.devTasks[m.tasksCursor].JiraKey
	if key == m.jiraDetailKey && !m.jiraDetailLoading {
		return nil
	}

	m.jiraDetailKey = key

	if cached, ok := m.jiraDetailCache[key]; ok {
		m.jiraDetail = cached
		m.jiraDetailLoading = false
		m.jiraDetailErr = nil
		m.updateDetailViewport()
		return nil
	}

	m.jiraDetailLoading = true
	m.jiraDetailErr = nil
	m.jiraDetail = nil
	m.updateDetailViewport()
	return fetchJiraDetail(m.jiraDetailFetcher, key)
}

// launchTmuxForTask opens a new tmux window named after the task's key,
// rooted at the task's worktree path. Pre-populates the shell with a
// claude /resume command so the user can hit Enter to pick up where
// they left off. Used for suspended tasks to quickly jump back in.
func launchTmuxForTask(jiraKey, worktreePath string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("tmux", "new-window", "-n", jiraKey, "-c", worktreePath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return taskActionDoneMsg{
				action: "tmux-launch",
				key:    jiraKey,
				err:    fmt.Errorf("%s: %s", err, string(out)),
			}
		}
		// Pre-populate the terminal with the resume command (typed, not executed).
		// -l ensures literal text — no tmux key-name interpretation.
		resumeCmd := fmt.Sprintf("claude /k-resume %s", jiraKey)
		sendKeys := exec.Command("tmux", "send-keys", "-l", "-t", jiraKey, resumeCmd)
		_ = sendKeys.Run()
		return taskActionDoneMsg{action: "tmux-launch", key: jiraKey}
	}
}

func runTaskGitSync() tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("task-ctl"); err != nil {
			return taskActionDoneMsg{
				action: "git-sync",
				err:    fmt.Errorf("task-ctl not found in PATH — install it to manage tasks"),
			}
		}
		cmd := exec.Command("task-ctl", "git-sync")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return taskActionDoneMsg{
				action: "git-sync",
				err:    fmt.Errorf("%s: %s", err, string(out)),
			}
		}
		return taskActionDoneMsg{action: "git-sync"}
	}
}
