package store

import (
	"context"
	"fmt"
	"time"
)

// DevTask represents a development task tracked by task-ctl.
type DevTask struct {
	ID            int
	JiraKey       string
	JiraSummary   string
	JiraType      string
	JiraPriority  string
	Repo          string
	Branch        string
	WorktreePath  string
	PlanPath      *string
	PRNumber      *int
	PRURL         *string
	Status        string
	TmuxWindow    *string
	GitDirtyCount int
	GitAhead      int
	GitBehind     int
	ContextDump   *string
	CreatedAt     time.Time
	LastActiveAt  time.Time
	SuspendedAt   *time.Time
	CompletedAt   *time.Time
}

// tasksTableExists checks if the tasks table has been created by task-ctl.
func (s *Store) tasksTableExists(ctx context.Context) bool {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='tasks'`,
	).Scan(&name)
	return err == nil && name == "tasks"
}

// ListDevTasks returns tasks filtered by status. Empty status returns active+suspended.
func (s *Store) ListDevTasks(ctx context.Context, status string) ([]DevTask, error) {
	if !s.tasksTableExists(ctx) {
		return nil, nil
	}

	var query string
	var args []any

	switch status {
	case "all":
		query = `SELECT id, jira_key, jira_summary, jira_type, jira_priority,
			repo, branch, worktree_path, plan_path, pr_number, pr_url,
			status, tmux_window, git_dirty_count, git_ahead, git_behind,
			context_dump, created_at, last_active_at, suspended_at, completed_at
			FROM tasks ORDER BY last_active_at DESC`
	case "active", "suspended", "completed", "archived":
		query = `SELECT id, jira_key, jira_summary, jira_type, jira_priority,
			repo, branch, worktree_path, plan_path, pr_number, pr_url,
			status, tmux_window, git_dirty_count, git_ahead, git_behind,
			context_dump, created_at, last_active_at, suspended_at, completed_at
			FROM tasks WHERE status = ? ORDER BY last_active_at DESC`
		args = append(args, status)
	default:
		query = `SELECT id, jira_key, jira_summary, jira_type, jira_priority,
			repo, branch, worktree_path, plan_path, pr_number, pr_url,
			status, tmux_window, git_dirty_count, git_ahead, git_behind,
			context_dump, created_at, last_active_at, suspended_at, completed_at
			FROM tasks WHERE status IN ('active', 'suspended') ORDER BY last_active_at DESC`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list dev tasks: %w", err)
	}
	defer rows.Close()

	var tasks []DevTask
	for rows.Next() {
		var t DevTask
		if err := rows.Scan(
			&t.ID, &t.JiraKey, &t.JiraSummary, &t.JiraType, &t.JiraPriority,
			&t.Repo, &t.Branch, &t.WorktreePath, &t.PlanPath, &t.PRNumber, &t.PRURL,
			&t.Status, &t.TmuxWindow, &t.GitDirtyCount, &t.GitAhead, &t.GitBehind,
			&t.ContextDump, &t.CreatedAt, &t.LastActiveAt, &t.SuspendedAt, &t.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan dev task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
