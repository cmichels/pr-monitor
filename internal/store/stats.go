package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// DailyStat holds per-user contribution metrics for a single day.
type DailyStat struct {
	Login            string
	Date             string // "2026-01-15"
	PRsCreated       int
	PRsMerged        int
	PRsReviewed      int
	CommentsGiven    int
	LinesAdded       int
	LinesRemoved     int
	ApprovalsGiven   int
	ChangesRequested int
}

// UserTotal holds aggregated metrics for a single user across all dates.
type UserTotal struct {
	Login            string
	PRsCreated       int
	PRsMerged        int
	PRsReviewed      int
	CommentsGiven    int
	ApprovalsGiven   int
	ChangesRequested int
	LinesAdded       int
	LinesRemoved     int
}

// UpsertDayStat inserts a daily stat row or updates it on conflict.
func (s *Store) UpsertDayStat(ctx context.Context, stat DailyStat) error {
	const query = `
		INSERT INTO contributor_stats (login, stat_date, prs_created, prs_merged, prs_reviewed,
			comments_given, lines_added, lines_removed, approvals_given, changes_requested)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(login, stat_date) DO UPDATE SET
			prs_created = excluded.prs_created,
			prs_merged = excluded.prs_merged,
			prs_reviewed = excluded.prs_reviewed,
			comments_given = excluded.comments_given,
			lines_added = excluded.lines_added,
			lines_removed = excluded.lines_removed,
			approvals_given = excluded.approvals_given,
			changes_requested = excluded.changes_requested,
			fetched_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.ExecContext(ctx, query,
		stat.Login, stat.Date, stat.PRsCreated, stat.PRsMerged, stat.PRsReviewed,
		stat.CommentsGiven, stat.LinesAdded, stat.LinesRemoved, stat.ApprovalsGiven, stat.ChangesRequested,
	)
	if err != nil {
		return fmt.Errorf("upsert day stat %s/%s: %w", stat.Login, stat.Date, err)
	}
	return nil
}

// GetAllStats returns all contributor stat rows ordered by date.
func (s *Store) GetAllStats(ctx context.Context) ([]DailyStat, error) {
	const query = `
		SELECT login, stat_date, prs_created, prs_merged, prs_reviewed,
		       comments_given, lines_added, lines_removed, approvals_given, changes_requested
		FROM contributor_stats
		ORDER BY stat_date ASC, login ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all stats: %w", err)
	}
	defer rows.Close()

	return scanDailyStats(rows)
}

// GetStatsByLogin returns all stat rows for a single user ordered by date.
func (s *Store) GetStatsByLogin(ctx context.Context, login string) ([]DailyStat, error) {
	const query = `
		SELECT login, stat_date, prs_created, prs_merged, prs_reviewed,
		       comments_given, lines_added, lines_removed, approvals_given, changes_requested
		FROM contributor_stats
		WHERE login = ?
		ORDER BY stat_date ASC
	`
	rows, err := s.db.QueryContext(ctx, query, login)
	if err != nil {
		return nil, fmt.Errorf("get stats by login %s: %w", login, err)
	}
	defer rows.Close()

	return scanDailyStats(rows)
}

// GetTopReviewers returns the top N users by PRs reviewed.
func (s *Store) GetTopReviewers(ctx context.Context, n int) ([]UserTotal, error) {
	const query = `
		SELECT login,
		       SUM(prs_created), SUM(prs_merged), SUM(prs_reviewed),
		       SUM(comments_given), SUM(approvals_given), SUM(changes_requested),
		       SUM(lines_added), SUM(lines_removed)
		FROM contributor_stats
		GROUP BY login
		ORDER BY SUM(prs_reviewed) DESC
		LIMIT ?
	`
	return s.queryUserTotals(ctx, query, n)
}

// GetTopAuthors returns the top N users by PRs merged.
func (s *Store) GetTopAuthors(ctx context.Context, n int) ([]UserTotal, error) {
	const query = `
		SELECT login,
		       SUM(prs_created), SUM(prs_merged), SUM(prs_reviewed),
		       SUM(comments_given), SUM(approvals_given), SUM(changes_requested),
		       SUM(lines_added), SUM(lines_removed)
		FROM contributor_stats
		GROUP BY login
		ORDER BY SUM(prs_merged) DESC
		LIMIT ?
	`
	return s.queryUserTotals(ctx, query, n)
}

// GetTeamMembers returns the cached list of team member logins from stats_meta.
func (s *Store) GetTeamMembers(ctx context.Context) ([]string, error) {
	val, ok, err := s.GetMeta(ctx, "team_members")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var members []string
	if err := json.Unmarshal([]byte(val), &members); err != nil {
		return nil, fmt.Errorf("unmarshal team members: %w", err)
	}
	return members, nil
}

// SetTeamMembers caches the list of team member logins in stats_meta as JSON.
func (s *Store) SetTeamMembers(ctx context.Context, members []string) error {
	data, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("marshal team members: %w", err)
	}
	return s.SetMeta(ctx, "team_members", string(data))
}

// GetMeta reads a value from the stats_meta key-value table.
func (s *Store) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM stats_meta WHERE key = ?`, key).Scan(&val)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get meta %q: %w", key, err)
	}
	return val, true, nil
}

// SetMeta writes a value to the stats_meta key-value table.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	const query = `
		INSERT INTO stats_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`
	_, err := s.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

// GetFetchedLogins returns distinct logins that have stat rows in the database.
func (s *Store) GetFetchedLogins(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT login FROM contributor_stats ORDER BY login`)
	if err != nil {
		return nil, fmt.Errorf("get fetched logins: %w", err)
	}
	defer rows.Close()

	var logins []string
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, fmt.Errorf("scan login: %w", err)
		}
		logins = append(logins, login)
	}
	return logins, rows.Err()
}

// queryUserTotals executes a GROUP BY query and returns UserTotal slices.
func (s *Store) queryUserTotals(ctx context.Context, query string, n int) ([]UserTotal, error) {
	rows, err := s.db.QueryContext(ctx, query, n)
	if err != nil {
		return nil, fmt.Errorf("query user totals: %w", err)
	}
	defer rows.Close()

	var totals []UserTotal
	for rows.Next() {
		var ut UserTotal
		if err := rows.Scan(
			&ut.Login,
			&ut.PRsCreated, &ut.PRsMerged, &ut.PRsReviewed,
			&ut.CommentsGiven, &ut.ApprovalsGiven, &ut.ChangesRequested,
			&ut.LinesAdded, &ut.LinesRemoved,
		); err != nil {
			return nil, fmt.Errorf("scan user total: %w", err)
		}
		totals = append(totals, ut)
	}
	return totals, rows.Err()
}

// scanDailyStats scans rows into a DailyStat slice.
func scanDailyStats(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]DailyStat, error) {
	var stats []DailyStat
	for rows.Next() {
		var ds DailyStat
		if err := rows.Scan(
			&ds.Login, &ds.Date, &ds.PRsCreated, &ds.PRsMerged, &ds.PRsReviewed,
			&ds.CommentsGiven, &ds.LinesAdded, &ds.LinesRemoved, &ds.ApprovalsGiven, &ds.ChangesRequested,
		); err != nil {
			return nil, fmt.Errorf("scan daily stat: %w", err)
		}
		stats = append(stats, ds)
	}
	return stats, rows.Err()
}
