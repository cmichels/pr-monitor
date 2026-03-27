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
	gen   int
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
	return func() tea.Msg {
		if loader == nil {
			return tasksDataLoadedMsg{gen: gen}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tasks, err := loader.ListDevTasks(ctx, "")
		return tasksDataLoadedMsg{tasks: tasks, gen: gen, err: err}
	}
}

func runTaskAction(action, jiraKey string) tea.Cmd {
	return func() tea.Msg {
		args := []string{action, jiraKey}
		cmd := exec.Command("task-ctl", args...)
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

func runTaskGitSync() tea.Cmd {
	return func() tea.Msg {
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
