package store

import (
	"context"
	"fmt"
	"time"
)

// JiraIssue represents a Jira issue tracked in the local database.
type JiraIssue struct {
	Key       string
	Summary   string
	Status    string
	StatusCat string
	Priority  string
	IssueType string
	Assignee  string
	Reporter  string
	Labels    string // JSON array string
	Source    string // "filter:13066", "filter:12562", "my_tasks"
	BrowseURL string
	FirstSeen time.Time
	UpdatedAt time.Time
}

const createJiraSchema = `
CREATE TABLE IF NOT EXISTS jira_user_stats (
    display_name TEXT NOT NULL,
    period       TEXT NOT NULL,
    created      INT NOT NULL DEFAULT 0,
    finished     INT NOT NULL DEFAULT 0,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (display_name, period)
);

CREATE TABLE IF NOT EXISTS jira_issues (
    issue_key   TEXT PRIMARY KEY,
    summary     TEXT NOT NULL,
    status      TEXT NOT NULL,
    status_cat  TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT '',
    issue_type  TEXT NOT NULL DEFAULT '',
    assignee    TEXT NOT NULL DEFAULT '',
    reporter    TEXT NOT NULL DEFAULT '',
    labels      TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',
    browse_url  TEXT NOT NULL DEFAULT '',
    first_seen  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_jira_source ON jira_issues(source);
`

// JiraUserStat holds aggregated Jira issue throughput for a single user.
type JiraUserStat struct {
	DisplayName   string
	Period        string // "ytd" or "month"
	CreatedCount  int
	FinishedCount int
}

// ReplaceJiraUserStats performs a full replace for the given period: deletes all
// existing rows for that period, then inserts the new stats in a single transaction.
func (s *Store) ReplaceJiraUserStats(ctx context.Context, period string, stats []JiraUserStat) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for jira user stats: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM jira_user_stats WHERE period = ?`, period); err != nil {
		return fmt.Errorf("delete jira user stats period %s: %w", period, err)
	}

	const insert = `INSERT INTO jira_user_stats (display_name, period, created, finished) VALUES (?, ?, ?, ?)`
	for _, st := range stats {
		if _, err := tx.ExecContext(ctx, insert, st.DisplayName, period, st.CreatedCount, st.FinishedCount); err != nil {
			return fmt.Errorf("insert jira user stat %s/%s: %w", st.DisplayName, period, err)
		}
	}

	return tx.Commit()
}

// GetJiraUserStats returns Jira user stats for the given period, ordered by
// total activity (created + finished) descending.
func (s *Store) GetJiraUserStats(ctx context.Context, period string) ([]JiraUserStat, error) {
	const query = `
		SELECT display_name, period, created, finished
		FROM jira_user_stats
		WHERE period = ?
		ORDER BY (created + finished) DESC, display_name ASC
	`
	rows, err := s.db.QueryContext(ctx, query, period)
	if err != nil {
		return nil, fmt.Errorf("get jira user stats %s: %w", period, err)
	}
	defer rows.Close()

	var result []JiraUserStat
	for rows.Next() {
		var st JiraUserStat
		if err := rows.Scan(&st.DisplayName, &st.Period, &st.CreatedCount, &st.FinishedCount); err != nil {
			return nil, fmt.Errorf("scan jira user stat: %w", err)
		}
		result = append(result, st)
	}
	return result, rows.Err()
}

// UpsertJiraIssue inserts a new Jira issue or updates an existing one.
func (s *Store) UpsertJiraIssue(ctx context.Context, issue JiraIssue) error {
	const query = `
		INSERT INTO jira_issues (issue_key, summary, status, status_cat, priority, issue_type,
			assignee, reporter, labels, source, browse_url, first_seen, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(issue_key) DO UPDATE SET
			summary = excluded.summary,
			status = excluded.status,
			status_cat = excluded.status_cat,
			priority = excluded.priority,
			issue_type = excluded.issue_type,
			assignee = excluded.assignee,
			reporter = excluded.reporter,
			labels = excluded.labels,
			source = excluded.source,
			browse_url = excluded.browse_url,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.ExecContext(ctx, query,
		issue.Key, issue.Summary, issue.Status, issue.StatusCat, issue.Priority,
		issue.IssueType, issue.Assignee, issue.Reporter, issue.Labels,
		issue.Source, issue.BrowseURL,
	)
	if err != nil {
		return fmt.Errorf("upsert jira issue %s: %w", issue.Key, err)
	}
	return nil
}

// GetJiraIssuesBySource returns all Jira issues for a given source, ordered by key.
func (s *Store) GetJiraIssuesBySource(ctx context.Context, source string) ([]JiraIssue, error) {
	const query = `
		SELECT issue_key, summary, status, status_cat, priority, issue_type,
		       assignee, reporter, labels, source, browse_url, first_seen, updated_at
		FROM jira_issues
		WHERE source = ?
		ORDER BY issue_key ASC
	`
	rows, err := s.db.QueryContext(ctx, query, source)
	if err != nil {
		return nil, fmt.Errorf("get jira issues by source %s: %w", source, err)
	}
	defer rows.Close()

	return s.scanJiraIssues(rows)
}

// GetAllJiraIssues returns all Jira issues ordered by source then key.
func (s *Store) GetAllJiraIssues(ctx context.Context) ([]JiraIssue, error) {
	const query = `
		SELECT issue_key, summary, status, status_cat, priority, issue_type,
		       assignee, reporter, labels, source, browse_url, first_seen, updated_at
		FROM jira_issues
		ORDER BY source ASC, issue_key ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all jira issues: %w", err)
	}
	defer rows.Close()

	return s.scanJiraIssues(rows)
}

// CleanupJiraIssues removes issues whose keys are not in the currentKeys set.
func (s *Store) CleanupJiraIssues(ctx context.Context, currentKeys []string) error {
	if len(currentKeys) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM jira_issues`)
		if err != nil {
			return fmt.Errorf("cleanup all jira issues: %w", err)
		}
		return nil
	}

	placeholders := ""
	args := make([]any, len(currentKeys))
	for i, key := range currentKeys {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args[i] = key
	}

	query := fmt.Sprintf(`DELETE FROM jira_issues WHERE issue_key NOT IN (%s)`, placeholders)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cleanup jira issues: %w", err)
	}
	return nil
}

// GetJiraIssueCounts returns issue counts grouped by source.
func (s *Store) GetJiraIssueCounts(ctx context.Context) (map[string]int, error) {
	const query = `SELECT source, COUNT(*) FROM jira_issues GROUP BY source`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get jira issue counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			return nil, fmt.Errorf("scan jira count: %w", err)
		}
		counts[source] = count
	}
	return counts, rows.Err()
}

// scanJiraIssues scans rows into JiraIssue slices.
func (s *Store) scanJiraIssues(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]JiraIssue, error) {
	var issues []JiraIssue
	for rows.Next() {
		var ji JiraIssue
		var firstSeen, updatedAt string
		if err := rows.Scan(
			&ji.Key, &ji.Summary, &ji.Status, &ji.StatusCat, &ji.Priority,
			&ji.IssueType, &ji.Assignee, &ji.Reporter, &ji.Labels,
			&ji.Source, &ji.BrowseURL, &firstSeen, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan jira issue: %w", err)
		}
		var err error
		if ji.FirstSeen, err = parseSQLiteTime(firstSeen); err != nil {
			return nil, fmt.Errorf("parse jira first_seen: %w", err)
		}
		if ji.UpdatedAt, err = parseSQLiteTime(updatedAt); err != nil {
			return nil, fmt.Errorf("parse jira updated_at: %w", err)
		}
		issues = append(issues, ji)
	}
	return issues, rows.Err()
}
