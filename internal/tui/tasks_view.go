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

var (
	taskDetailLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	taskDetailValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	taskDetailDimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))
)

// renderTaskDetailPanel renders the detail panel for the selected task.
func renderTaskDetailPanel(m Model) string {
	if len(m.devTasks) == 0 || m.tasksCursor >= len(m.devTasks) {
		return taskDetailDimStyle.Render("  Select a task to view details")
	}

	t := m.devTasks[m.tasksCursor]
	var b strings.Builder

	// Header: key + status
	statusStyle := taskCompletedStyle
	switch t.Status {
	case "active":
		statusStyle = taskActiveStyle
	case "suspended":
		statusStyle = taskSuspendedStyle
	}

	b.WriteString(taskHeaderStyle.Render(t.JiraKey))
	b.WriteString("  ")
	b.WriteString(statusStyle.Render(t.Status))
	b.WriteString("\n")

	if t.JiraSummary != "" {
		b.WriteString(taskDetailValueStyle.Render(t.JiraSummary))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Jira metadata
	if t.JiraType != "" || t.JiraPriority != "" {
		b.WriteString(taskDetailLabelStyle.Render("Type: "))
		b.WriteString(taskDetailValueStyle.Render(t.JiraType))
		b.WriteString("  ")
		b.WriteString(taskDetailLabelStyle.Render("Priority: "))
		b.WriteString(taskDetailValueStyle.Render(t.JiraPriority))
		b.WriteString("\n")
	}

	// Repo + branch
	b.WriteString(taskDetailLabelStyle.Render("Repo: "))
	b.WriteString(taskDetailValueStyle.Render(t.Repo))
	b.WriteString("\n")
	b.WriteString(taskDetailLabelStyle.Render("Branch: "))
	b.WriteString(taskDetailValueStyle.Render(t.Branch))
	b.WriteString("\n")
	b.WriteString(taskDetailLabelStyle.Render("Worktree: "))
	b.WriteString(taskDetailValueStyle.Render(t.WorktreePath))
	b.WriteString("\n")

	if t.PlanPath != nil {
		b.WriteString(taskDetailLabelStyle.Render("Plan: "))
		b.WriteString(taskDetailValueStyle.Render(*t.PlanPath))
		b.WriteString("\n")
	}

	// PR link
	if t.PRNumber != nil {
		b.WriteString(taskDetailLabelStyle.Render("PR: "))
		pr := fmt.Sprintf("#%d", *t.PRNumber)
		if t.PRURL != nil {
			pr += fmt.Sprintf(" (%s)", *t.PRURL)
		}
		b.WriteString(taskDetailValueStyle.Render(pr))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Git state
	b.WriteString(taskDetailLabelStyle.Render("Git State"))
	b.WriteString("\n")
	gitLine := fmt.Sprintf("  Dirty: %d  Ahead: %d  Behind: %d", t.GitDirtyCount, t.GitAhead, t.GitBehind)
	b.WriteString(taskDetailValueStyle.Render(gitLine))
	b.WriteString("\n\n")

	// Timestamps
	b.WriteString(taskDetailLabelStyle.Render("Timestamps"))
	b.WriteString("\n")
	b.WriteString(taskDetailDimStyle.Render(fmt.Sprintf("  Created:     %s", t.CreatedAt.Format("2006-01-02 15:04"))))
	b.WriteString("\n")
	b.WriteString(taskDetailDimStyle.Render(fmt.Sprintf("  Last Active: %s (%s)", t.LastActiveAt.Format("2006-01-02 15:04"), shortAge(t.LastActiveAt))))
	b.WriteString("\n")
	if t.SuspendedAt != nil {
		b.WriteString(taskDetailDimStyle.Render(fmt.Sprintf("  Suspended:   %s", t.SuspendedAt.Format("2006-01-02 15:04"))))
		b.WriteString("\n")
	}
	if t.CompletedAt != nil {
		b.WriteString(taskDetailDimStyle.Render(fmt.Sprintf("  Completed:   %s", t.CompletedAt.Format("2006-01-02 15:04"))))
		b.WriteString("\n")
	}

	// Context dump
	if t.ContextDump != nil && *t.ContextDump != "" {
		b.WriteString("\n")
		b.WriteString(taskDetailLabelStyle.Render("Context Dump"))
		b.WriteString("\n")
		b.WriteString(taskDetailValueStyle.Render(*t.ContextDump))
		b.WriteString("\n")
	}

	return b.String()
}
