package store

import (
	"context"
	"testing"
	"time"
)

const createTasksTable = `
CREATE TABLE IF NOT EXISTS tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    jira_key        TEXT NOT NULL,
    jira_summary    TEXT NOT NULL DEFAULT '',
    jira_type       TEXT NOT NULL DEFAULT '',
    jira_priority   TEXT NOT NULL DEFAULT '',
    jira_status     TEXT NOT NULL DEFAULT '',
    repo            TEXT NOT NULL,
    branch          TEXT NOT NULL,
    worktree_path   TEXT NOT NULL UNIQUE,
    plan_path       TEXT,
    pr_number       INTEGER,
    pr_url          TEXT,
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK(status IN ('active','suspended','completed','archived')),
    tmux_window     TEXT,
    git_dirty_count INTEGER DEFAULT 0,
    git_ahead       INTEGER DEFAULT 0,
    git_behind      INTEGER DEFAULT 0,
    context_dump    TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    suspended_at    DATETIME,
    completed_at    DATETIME
);`

func seedTasks(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, createTasksTable)
	if err != nil {
		t.Fatalf("create tasks table: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (jira_key, jira_summary, jira_type, jira_priority,
			repo, branch, worktree_path, status, created_at, last_active_at)
		VALUES
			('OP-1234', 'Add webhook retry', 'Story', 'Medium',
			 'org/repo', 'feature/OP-1234_webhooks', '/tmp/wt1', 'active', ?, ?),
			('OP-5678', 'Fix null pointer', 'Bug', 'High',
			 'org/repo', 'bug/OP-5678_null', '/tmp/wt2', 'suspended', ?, ?),
			('OP-9999', 'Cleanup old code', 'Task', 'Low',
			 'org/repo', 'feature/OP-9999_cleanup', '/tmp/wt3', 'completed', ?, ?)`,
		now, now, now, now, now, now)
	if err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
}

func TestListDevTasks_Default(t *testing.T) {
	s := newTestStore(t)
	seedTasks(t, s)
	tasks, err := s.ListDevTasks(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Default: active + suspended.
	if len(tasks) != 2 {
		t.Fatalf("expected 2, got %d", len(tasks))
	}
}

func TestListDevTasks_Active(t *testing.T) {
	s := newTestStore(t)
	seedTasks(t, s)
	tasks, err := s.ListDevTasks(context.Background(), "active")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1, got %d", len(tasks))
	}
	if tasks[0].JiraKey != "OP-1234" {
		t.Errorf("expected OP-1234, got %s", tasks[0].JiraKey)
	}
}

func TestListDevTasks_All(t *testing.T) {
	s := newTestStore(t)
	seedTasks(t, s)
	tasks, err := s.ListDevTasks(context.Background(), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3, got %d", len(tasks))
	}
}

func TestListDevTasks_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, createTasksTable)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListDevTasks(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0, got %d", len(tasks))
	}
}

func TestListDevTasks_NoTable(t *testing.T) {
	s := newTestStore(t)
	// Don't create the tasks table.
	tasks, err := s.ListDevTasks(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 (no table), got %d", len(tasks))
	}
}
