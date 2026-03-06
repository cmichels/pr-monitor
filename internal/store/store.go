package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// PR represents a pull request tracked in the local database.
type PR struct {
	ID               int64
	PRID             string     // GitHub node ID (globally unique)
	Repo             string     // "org/repo-name"
	Number           int
	Title            string
	Author           string
	URL              string     // HTML URL
	Role             string     // "reviewer" or "author"
	FilesChanged     int
	CIStatus         string     // "passing", "failing", "pending", "unknown"
	ReviewerStatus   string     // "pending", "approved", "commented", "changes_requested"
	IsDraft          bool       // true if PR is a draft (authored PRs only)
	FirstSeen        time.Time
	LastSeen         time.Time
	NotifiedAt       *time.Time
	Status           string     // "pending", "dismissed", "reviewed"
	LastActivityAt   *time.Time
	LastActivityType *string    // "approved", "commented", "changes_requested"
	LastActivityBy   *string
}

// Store provides SQLite-backed persistence for tracked pull requests.
type Store struct {
	db *sql.DB
}

// TODO: v2 — add schema_version table and migration logic
const createSchema = `
CREATE TABLE IF NOT EXISTS pull_requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id           TEXT NOT NULL UNIQUE,
    repo            TEXT NOT NULL,
    number          INTEGER NOT NULL,
    title           TEXT NOT NULL,
    author          TEXT NOT NULL,
    url             TEXT NOT NULL,
    role            TEXT NOT NULL CHECK(role IN ('reviewer', 'author')),
    files_changed   INTEGER DEFAULT 0,
    ci_status       TEXT DEFAULT 'unknown',
    first_seen      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notified_at     DATETIME,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'dismissed', 'reviewed')),
    last_activity_at   DATETIME,
    last_activity_type TEXT,
    last_activity_by   TEXT,
    reviewer_status    TEXT DEFAULT 'pending',
    UNIQUE(repo, number)
);

CREATE INDEX IF NOT EXISTS idx_pr_role_status ON pull_requests(role, status);
CREATE INDEX IF NOT EXISTS idx_pr_repo ON pull_requests(repo);

CREATE TABLE IF NOT EXISTS contributor_stats (
    login           TEXT NOT NULL,
    stat_date       TEXT NOT NULL,
    prs_created     INTEGER NOT NULL DEFAULT 0,
    prs_merged      INTEGER NOT NULL DEFAULT 0,
    prs_reviewed    INTEGER NOT NULL DEFAULT 0,
    comments_given  INTEGER NOT NULL DEFAULT 0,
    lines_added     INTEGER NOT NULL DEFAULT 0,
    lines_removed   INTEGER NOT NULL DEFAULT 0,
    approvals_given      INTEGER NOT NULL DEFAULT 0,
    changes_requested    INTEGER NOT NULL DEFAULT 0,
    fetched_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(login, stat_date)
);

CREATE INDEX IF NOT EXISTS idx_stats_date  ON contributor_stats(stat_date);
CREATE INDEX IF NOT EXISTS idx_stats_login ON contributor_stats(login);

CREATE TABLE IF NOT EXISTS stats_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// New opens the SQLite database at dbPath, enables WAL mode (unless in-memory),
// creates the schema, and returns a ready-to-use Store.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// WAL mode allows concurrent reads during writes. Not supported for :memory:.
	if dbPath != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL mode: %w", err)
		}
	}

	// Busy timeout: wait up to 5s for locks instead of failing immediately.
	// Multiple goroutines (GitHub poller, Jira poller, stats backfill) write concurrently.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), createSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Create Jira schema.
	if _, err := db.ExecContext(context.Background(), createJiraSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create jira schema: %w", err)
	}

	// Migration: add reviewer_status column for existing databases.
	_, _ = db.Exec("ALTER TABLE pull_requests ADD COLUMN reviewer_status TEXT DEFAULT 'pending'")

	// Migration: add is_draft column for existing databases.
	_, _ = db.Exec("ALTER TABLE pull_requests ADD COLUMN is_draft INTEGER DEFAULT 0")

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertPR inserts a new PR or updates an existing one (matched by pr_id).
// On update: refreshes title, author, files_changed, ci_status, last_seen, url.
// On insert: sets all fields, first_seen = now, status = 'pending'.
// Does NOT overwrite status, notified_at, or first_seen on update.
func (s *Store) UpsertPR(ctx context.Context, pr PR) error {
	const query = `
		INSERT INTO pull_requests (pr_id, repo, number, title, author, url, role, files_changed, ci_status, reviewer_status, is_draft, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(pr_id) DO UPDATE SET
			title = excluded.title,
			author = excluded.author,
			url = excluded.url,
			files_changed = excluded.files_changed,
			ci_status = excluded.ci_status,
			reviewer_status = excluded.reviewer_status,
			is_draft = excluded.is_draft,
			last_seen = CURRENT_TIMESTAMP
	`
	reviewerStatus := pr.ReviewerStatus
	if reviewerStatus == "" {
		reviewerStatus = "pending"
	}
	isDraft := 0
	if pr.IsDraft {
		isDraft = 1
	}
	_, err := s.db.ExecContext(ctx, query,
		pr.PRID, pr.Repo, pr.Number, pr.Title, pr.Author, pr.URL, pr.Role, pr.FilesChanged, pr.CIStatus, reviewerStatus, isDraft,
	)
	if err != nil {
		return fmt.Errorf("upsert PR %s: %w", pr.PRID, err)
	}
	return nil
}

// Dismiss marks a PR as dismissed so it no longer appears in active lists.
func (s *Store) Dismiss(ctx context.Context, prID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pull_requests SET status = 'dismissed' WHERE pr_id = ?`, prID)
	if err != nil {
		return fmt.Errorf("dismiss PR %s: %w", prID, err)
	}
	return nil
}

// MarkReviewed marks a PR as reviewed.
func (s *Store) MarkReviewed(ctx context.Context, prID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pull_requests SET status = 'reviewed' WHERE pr_id = ?`, prID)
	if err != nil {
		return fmt.Errorf("mark reviewed PR %s: %w", prID, err)
	}
	return nil
}

// MarkNotified records the current time as the last notification time for a PR.
func (s *Store) MarkNotified(ctx context.Context, prID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pull_requests SET notified_at = CURRENT_TIMESTAMP WHERE pr_id = ?`, prID)
	if err != nil {
		return fmt.Errorf("mark notified PR %s: %w", prID, err)
	}
	return nil
}

// UpdateActivity sets the last_activity_* fields on a PR. If activityAt is newer
// than the current notified_at, it also clears notified_at so the PR appears in
// FindNew() again and triggers a re-notification for the new event.
func (s *Store) UpdateActivity(ctx context.Context, prID string, activityAt time.Time, activityType, activityBy string) error {
	const query = `
		UPDATE pull_requests SET
			last_activity_at = ?,
			last_activity_type = ?,
			last_activity_by = ?,
			notified_at = CASE
				WHEN notified_at IS NULL THEN NULL
				WHEN ? > notified_at THEN NULL
				ELSE notified_at
			END
		WHERE pr_id = ?
	`
	_, err := s.db.ExecContext(ctx, query,
		activityAt.UTC(), activityType, activityBy,
		activityAt.UTC(), prID,
	)
	if err != nil {
		return fmt.Errorf("update activity PR %s: %w", prID, err)
	}
	return nil
}

// GetPendingByRole returns all PRs with status='pending' for the given role,
// ordered by first_seen ascending (oldest first).
func (s *Store) GetPendingByRole(ctx context.Context, role string) ([]PR, error) {
	const query = `
		SELECT id, pr_id, repo, number, title, author, url, role, files_changed, ci_status,
		       first_seen, last_seen, notified_at, status, last_activity_at, last_activity_type, last_activity_by,
		       reviewer_status, is_draft
		FROM pull_requests
		WHERE role = ? AND status = 'pending'
		ORDER BY first_seen ASC
	`
	return s.scanPRs(s.db.QueryContext(ctx, query, role))
}

// FindNew returns PRs that have not yet been notified (notified_at IS NULL)
// and are still pending, for the given role.
func (s *Store) FindNew(ctx context.Context, role string) ([]PR, error) {
	const query = `
		SELECT id, pr_id, repo, number, title, author, url, role, files_changed, ci_status,
		       first_seen, last_seen, notified_at, status, last_activity_at, last_activity_type, last_activity_by,
		       reviewer_status, is_draft
		FROM pull_requests
		WHERE role = ? AND status = 'pending' AND notified_at IS NULL
		ORDER BY first_seen ASC
	`
	return s.scanPRs(s.db.QueryContext(ctx, query, role))
}

// GetCounts returns the count of pending PRs for each role.
func (s *Store) GetCounts(ctx context.Context) (reviewCount, authoredCount int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE role = 'reviewer' AND status = 'pending'`,
	).Scan(&reviewCount)
	if err != nil {
		return 0, 0, fmt.Errorf("count reviewer PRs: %w", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE role = 'author' AND status = 'pending'`,
	).Scan(&authoredCount)
	if err != nil {
		return 0, 0, fmt.Errorf("count authored PRs: %w", err)
	}

	return reviewCount, authoredCount, nil
}

// OldestPendingAge returns the age of the oldest pending reviewer PR.
// Returns 0 if there are no pending reviewer PRs.
func (s *Store) OldestPendingAge(ctx context.Context) (time.Duration, error) {
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(first_seen) FROM pull_requests WHERE role = 'reviewer' AND status = 'pending'`,
	).Scan(&raw)
	if err != nil {
		return 0, fmt.Errorf("oldest pending age: %w", err)
	}
	if !raw.Valid {
		return 0, nil
	}
	t, err := parseSQLiteTime(raw.String)
	if err != nil {
		return 0, fmt.Errorf("oldest pending age: parse time: %w", err)
	}
	return time.Since(t), nil
}

// Cleanup removes pending PRs whose pr_id is not in the currentPRIDs set.
// This handles PRs that were merged or closed on GitHub. Dismissed PRs are preserved.
func (s *Store) Cleanup(ctx context.Context, currentPRIDs []string) error {
	if len(currentPRIDs) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM pull_requests WHERE status = 'pending'`)
		if err != nil {
			return fmt.Errorf("cleanup all pending: %w", err)
		}
		return nil
	}

	// Build placeholders for the IN clause.
	placeholders := ""
	args := make([]any, len(currentPRIDs)+1)
	args[0] = "pending"
	for i, id := range currentPRIDs {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(
		`DELETE FROM pull_requests WHERE status = ? AND pr_id NOT IN (%s)`,
		placeholders,
	)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cleanup pending: %w", err)
	}
	return nil
}

// SQLite datetime formats used by CURRENT_TIMESTAMP and Go's time formatting.
var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999Z",
	time.RFC3339Nano,
	time.RFC3339,
}

// parseSQLiteTime parses a datetime string as returned by modernc.org/sqlite.
func parseSQLiteTime(s string) (time.Time, error) {
	for _, f := range sqliteTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse SQLite time %q", s)
}

// scanNullableTime scans a sql.NullString into a *time.Time, returning nil for NULL.
func scanNullableTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := parseSQLiteTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// scanPRs scans rows from a query into a slice of PR structs.
// modernc.org/sqlite returns DATETIME columns as strings, so we scan into
// NullString and parse manually.
func (s *Store) scanPRs(rows *sql.Rows, err error) ([]PR, error) {
	if err != nil {
		return nil, fmt.Errorf("query PRs: %w", err)
	}
	defer rows.Close()

	var prs []PR
	for rows.Next() {
		var pr PR
		var firstSeen, lastSeen string
		var notifiedAt sql.NullString
		var lastActivityAt sql.NullString
		var lastActivityType sql.NullString
		var lastActivityBy sql.NullString
		var reviewerStatus sql.NullString
		var isDraft int

		if err := rows.Scan(
			&pr.ID, &pr.PRID, &pr.Repo, &pr.Number, &pr.Title, &pr.Author,
			&pr.URL, &pr.Role, &pr.FilesChanged, &pr.CIStatus,
			&firstSeen, &lastSeen, &notifiedAt, &pr.Status,
			&lastActivityAt, &lastActivityType, &lastActivityBy,
			&reviewerStatus, &isDraft,
		); err != nil {
			return nil, fmt.Errorf("scan PR row: %w", err)
		}

		if pr.FirstSeen, err = parseSQLiteTime(firstSeen); err != nil {
			return nil, fmt.Errorf("parse first_seen: %w", err)
		}
		if pr.LastSeen, err = parseSQLiteTime(lastSeen); err != nil {
			return nil, fmt.Errorf("parse last_seen: %w", err)
		}
		if pr.NotifiedAt, err = scanNullableTime(notifiedAt); err != nil {
			return nil, fmt.Errorf("parse notified_at: %w", err)
		}
		if pr.LastActivityAt, err = scanNullableTime(lastActivityAt); err != nil {
			return nil, fmt.Errorf("parse last_activity_at: %w", err)
		}
		if lastActivityType.Valid {
			pr.LastActivityType = &lastActivityType.String
		}
		if lastActivityBy.Valid {
			pr.LastActivityBy = &lastActivityBy.String
		}
		if reviewerStatus.Valid {
			pr.ReviewerStatus = reviewerStatus.String
		} else {
			pr.ReviewerStatus = "pending"
		}
		pr.IsDraft = isDraft != 0

		prs = append(prs, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PR rows: %w", err)
	}
	return prs, nil
}
