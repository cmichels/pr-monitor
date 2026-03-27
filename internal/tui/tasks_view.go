package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/chrismichels/pr-monitor/internal/store"
)

var (
	taskActiveStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	taskSuspendedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	taskCompletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	taskHeaderStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	taskCursorStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	taskHelpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func renderTasksTab(m Model) string {
	if m.tasksLoading && len(m.devTasks) == 0 {
		return fmt.Sprintf("\n  %s Loading tasks...", m.spinner.View())
	}

	if len(m.devTasks) == 0 {
		return "\n  No tracked tasks. Use /worktree to create one, or run task-ctl scan."
	}

	var b strings.Builder

	// Header row.
	header := fmt.Sprintf("  %-12s %-11s %-18s %-35s %-8s %-6s %-6s %-8s %s",
		"KEY", "STATUS", "REPO", "BRANCH", "PR", "DIRTY", "AHEAD", "BEHIND", "LAST ACTIVE")
	b.WriteString(taskHeaderStyle.Render(header))
	b.WriteString("\n")

	for i, t := range m.devTasks {
		row := formatTaskRow(t)
		if i == m.tasksCursor {
			b.WriteString(taskCursorStyle.Render(row))
		} else {
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	// Help line.
	b.WriteString("\n")
	b.WriteString(taskHelpStyle.Render("  Enter:resume  s:suspend  d:remove  c:complete  g:git-sync  r:refresh"))

	return b.String()
}

func formatTaskRow(t store.DevTask) string {
	statusStyle := taskCompletedStyle
	switch t.Status {
	case "active":
		statusStyle = taskActiveStyle
	case "suspended":
		statusStyle = taskSuspendedStyle
	}

	// Shorten repo: "Stark-Tech-Group/alarm-service" → "alarm-service".
	repo := t.Repo
	if parts := strings.Split(repo, "/"); len(parts) == 2 {
		repo = parts[1]
	}
	if len(repo) > 18 {
		repo = repo[:15] + "..."
	}

	// Truncate branch.
	branch := t.Branch
	if len(branch) > 35 {
		branch = branch[:32] + "..."
	}

	// PR display.
	pr := "-"
	if t.PRNumber != nil {
		pr = fmt.Sprintf("#%d", *t.PRNumber)
	}

	// Relative time.
	age := shortAge(t.LastActiveAt)

	return fmt.Sprintf("  %-12s %s %-18s %-35s %-8s %-6d %-6d %-8d %s",
		t.JiraKey,
		statusStyle.Render(fmt.Sprintf("%-11s", t.Status)),
		repo,
		branch,
		pr,
		t.GitDirtyCount,
		t.GitAhead,
		t.GitBehind,
		age,
	)
}

func shortAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
