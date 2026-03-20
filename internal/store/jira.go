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

CREATE TABLE IF NOT EXISTS tracked_epics (
    epic_key   TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    active     INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
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

// GetJiraIssuesBySourcePrefix returns all Jira issues whose source starts with the given prefix.
func (s *Store) GetJiraIssuesBySourcePrefix(ctx context.Context, prefix string) ([]JiraIssue, error) {
	const query = `
		SELECT issue_key, summary, status, status_cat, priority, issue_type,
		       assignee, reporter, labels, source, browse_url, first_seen, updated_at
		FROM jira_issues
		WHERE source LIKE ?
		ORDER BY issue_key ASC
	`
	rows, err := s.db.QueryContext(ctx, query, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("get jira issues by source prefix %s: %w", prefix, err)
	}
	defer rows.Close()

	return s.scanJiraIssues(rows)
}

// TrackedEpic represents a Jira epic that the user wants to track.
type TrackedEpic struct {
	EpicKey   string
	Name      string
	Active    bool
	SortOrder int
}

// SyncTrackedEpics seeds tracked epics from config. Inserts new epics (INSERT OR IGNORE)
// and updates the name on existing ones. Never touches the active flag so user overrides persist.
func (s *Store) SyncTrackedEpics(ctx context.Context, epics []TrackedEpic) error {
	const query = `
		INSERT INTO tracked_epics (epic_key, name, sort_order)
		VALUES (?, ?, ?)
		ON CONFLICT(epic_key) DO UPDATE SET
			name = excluded.name,
			sort_order = excluded.sort_order
	`
	for _, e := range epics {
		if _, err := s.db.ExecContext(ctx, query, e.EpicKey, e.Name, e.SortOrder); err != nil {
			return fmt.Errorf("sync tracked epic %s: %w", e.EpicKey, err)
		}
	}
	return nil
}

// GetActiveTrackedEpics returns all epics with active=1, ordered by sort_order then key.
func (s *Store) GetActiveTrackedEpics(ctx context.Context) ([]TrackedEpic, error) {
	const query = `
		SELECT epic_key, name, active, sort_order
		FROM tracked_epics
		WHERE active = 1
		ORDER BY sort_order ASC, epic_key ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get active tracked epics: %w", err)
	}
	defer rows.Close()

	var result []TrackedEpic
	for rows.Next() {
		var te TrackedEpic
		var active int
		if err := rows.Scan(&te.EpicKey, &te.Name, &active, &te.SortOrder); err != nil {
			return nil, fmt.Errorf("scan tracked epic: %w", err)
		}
		te.Active = active != 0
		result = append(result, te)
	}
	return result, rows.Err()
}

// AddTrackedEpic inserts a new tracked epic with the next sort_order. If the epic
// already exists (e.g. was previously removed and re-added), it reactivates it.
func (s *Store) AddTrackedEpic(ctx context.Context, epicKey, name string) error {
	const query = `
		INSERT INTO tracked_epics (epic_key, name, active, sort_order)
		VALUES (?, ?, 1, COALESCE((SELECT MAX(sort_order) + 1 FROM tracked_epics), 0))
		ON CONFLICT(epic_key) DO UPDATE SET
			name = excluded.name,
			active = 1
	`
	_, err := s.db.ExecContext(ctx, query, epicKey, name)
	if err != nil {
		return fmt.Errorf("add tracked epic %s: %w", epicKey, err)
	}
	return nil
}

// SetEpicActive sets the active flag for a tracked epic.
func (s *Store) SetEpicActive(ctx context.Context, epicKey string, active bool) error {
	val := 0
	if active {
		val = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tracked_epics SET active = ? WHERE epic_key = ?`, val, epicKey)
	if err != nil {
		return fmt.Errorf("set epic active %s: %w", epicKey, err)
	}
	return nil
}

// RemoveTrackedEpic deletes a tracked epic and its associated child issues.
func (s *Store) RemoveTrackedEpic(ctx context.Context, epicKey string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for remove epic: %w", err)
	}
	defer tx.Rollback()

	// Delete child issues sourced from this epic.
	source := "epic:" + epicKey
	if _, err := tx.ExecContext(ctx, `DELETE FROM jira_issues WHERE source = ?`, source); err != nil {
		return fmt.Errorf("delete epic issues %s: %w", epicKey, err)
	}

	// Delete the tracked epic itself.
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracked_epics WHERE epic_key = ?`, epicKey); err != nil {
		return fmt.Errorf("delete tracked epic %s: %w", epicKey, err)
	}

	return tx.Commit()
}

// CleanupJiraIssuesByPrefix removes epic-sourced issues whose keys are not in currentKeys.
// prefix should be "epic:" — only rows with source LIKE 'epic:%' are affected.
func (s *Store) CleanupJiraIssuesByPrefix(ctx context.Context, prefix string, currentKeys []string) error {
	if len(currentKeys) == 0 {
		query := `DELETE FROM jira_issues WHERE source LIKE ?`
		_, err := s.db.ExecContext(ctx, query, prefix+"%")
		if err != nil {
			return fmt.Errorf("cleanup jira issues by prefix %s: %w", prefix, err)
		}
		return nil
	}

	placeholders := ""
	args := make([]any, 0, len(currentKeys)+1)
	args = append(args, prefix+"%")
	for i, key := range currentKeys {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, key)
	}

	query := fmt.Sprintf(`DELETE FROM jira_issues WHERE source LIKE ? AND issue_key NOT IN (%s)`, placeholders)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cleanup jira issues by prefix %s: %w", prefix, err)
	}
	return nil
}

// CleanupJiraIssuesNonEpic removes filter/my_tasks issues whose keys are not in currentKeys.
// Only rows with source NOT LIKE 'epic:%' AND NOT LIKE 'sprint:%' are affected,
// so epic and sprint data are left untouched (they have their own cleanup paths).
func (s *Store) CleanupJiraIssuesNonEpic(ctx context.Context, currentKeys []string) error {
	const sourceFilter = `source NOT LIKE 'epic:%' AND source NOT LIKE 'sprint:%'`

	if len(currentKeys) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM jira_issues WHERE `+sourceFilter)
		if err != nil {
			return fmt.Errorf("cleanup non-epic jira issues: %w", err)
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

	query := fmt.Sprintf(`DELETE FROM jira_issues WHERE %s AND issue_key NOT IN (%s)`, sourceFilter, placeholders)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cleanup non-epic jira issues: %w", err)
	}
	return nil
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
