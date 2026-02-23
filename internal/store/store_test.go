package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func samplePR(prid, repo string, number int, role string) PR {
	return PR{
		PRID:         prid,
		Repo:         repo,
		Number:       number,
		Title:        "Fix the thing",
		Author:       "octocat",
		URL:          "https://github.com/" + repo + "/pull/" + string(rune('0'+number)),
		Role:         role,
		FilesChanged: 3,
		CIStatus:     "passing",
	}
}

func TestNew_CreatesSchema(t *testing.T) {
	s := newTestStore(t)

	// Verify the table exists by running a query against it.
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM pull_requests").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpsertPR_Insert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pr := samplePR("node-1", "org/repo", 42, "reviewer")
	err := s.UpsertPR(ctx, pr)
	require.NoError(t, err)

	prs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	require.Len(t, prs, 1)

	got := prs[0]
	assert.Equal(t, "node-1", got.PRID)
	assert.Equal(t, "org/repo", got.Repo)
	assert.Equal(t, 42, got.Number)
	assert.Equal(t, "Fix the thing", got.Title)
	assert.Equal(t, "octocat", got.Author)
	assert.Equal(t, "reviewer", got.Role)
	assert.Equal(t, 3, got.FilesChanged)
	assert.Equal(t, "passing", got.CIStatus)
	assert.Equal(t, "pending", got.Status)
	assert.False(t, got.FirstSeen.IsZero())
	assert.False(t, got.LastSeen.IsZero())
	assert.Nil(t, got.NotifiedAt)
}

func TestUpsertPR_Update_PreservesFirstSeenAndStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pr := samplePR("node-1", "org/repo", 42, "reviewer")
	require.NoError(t, s.UpsertPR(ctx, pr))

	// Get the original first_seen.
	prs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	originalFirstSeen := prs[0].FirstSeen
	originalLastSeen := prs[0].LastSeen

	// Dismiss the PR so we can verify status is preserved on upsert.
	require.NoError(t, s.Dismiss(ctx, "node-1"))

	// Small delay to ensure timestamps differ.
	time.Sleep(10 * time.Millisecond)

	// Upsert again with changed title.
	pr.Title = "Fix the thing v2"
	pr.FilesChanged = 10
	pr.CIStatus = "failing"
	require.NoError(t, s.UpsertPR(ctx, pr))

	// Read directly since it's dismissed (GetPendingByRole won't return it).
	var got PR
	var firstSeenStr, lastSeenStr string
	var notifiedAt, lastActivityAt, lastActivityType, lastActivityBy sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT id, pr_id, repo, number, title, author, url, role, files_changed, ci_status,
		        first_seen, last_seen, notified_at, status, last_activity_at, last_activity_type, last_activity_by
		 FROM pull_requests WHERE pr_id = ?`, "node-1",
	).Scan(&got.ID, &got.PRID, &got.Repo, &got.Number, &got.Title, &got.Author,
		&got.URL, &got.Role, &got.FilesChanged, &got.CIStatus,
		&firstSeenStr, &lastSeenStr, &notifiedAt, &got.Status,
		&lastActivityAt, &lastActivityType, &lastActivityBy)
	require.NoError(t, err)

	gotFirstSeen, err := parseSQLiteTime(firstSeenStr)
	require.NoError(t, err)
	gotLastSeen, err := parseSQLiteTime(lastSeenStr)
	require.NoError(t, err)

	// Title, files_changed, ci_status should be updated.
	assert.Equal(t, "Fix the thing v2", got.Title)
	assert.Equal(t, 10, got.FilesChanged)
	assert.Equal(t, "failing", got.CIStatus)

	// first_seen must NOT change, status must stay dismissed.
	assert.Equal(t, originalFirstSeen.Unix(), gotFirstSeen.Unix())
	assert.Equal(t, "dismissed", got.Status)

	// last_seen should advance.
	assert.True(t, gotLastSeen.Unix() >= originalLastSeen.Unix())
}

func TestGetPendingByRole_FiltersByRoleAndStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("a1", "org/c", 3, "author")))

	// Dismiss one reviewer PR.
	require.NoError(t, s.Dismiss(ctx, "r1"))

	reviewerPRs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	assert.Len(t, reviewerPRs, 1)
	assert.Equal(t, "r2", reviewerPRs[0].PRID)

	authorPRs, err := s.GetPendingByRole(ctx, "author")
	require.NoError(t, err)
	assert.Len(t, authorPRs, 1)
	assert.Equal(t, "a1", authorPRs[0].PRID)
}

func TestFindNew_ReturnsOnlyUnnotifiedPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r3", "org/c", 3, "reviewer")))

	// Notify r1, dismiss r3.
	require.NoError(t, s.MarkNotified(ctx, "r1"))
	require.NoError(t, s.Dismiss(ctx, "r3"))

	newPRs, err := s.FindNew(ctx, "reviewer")
	require.NoError(t, err)
	assert.Len(t, newPRs, 1)
	assert.Equal(t, "r2", newPRs[0].PRID)
}

func TestDismiss(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.Dismiss(ctx, "r1"))

	prs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	assert.Empty(t, prs)

	// Verify it's actually dismissed, not deleted.
	var status string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM pull_requests WHERE pr_id = ?`, "r1").Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "dismissed", status)
}

func TestMarkReviewed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.MarkReviewed(ctx, "r1"))

	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM pull_requests WHERE pr_id = ?`, "r1").Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "reviewed", status)
}

func TestMarkNotified(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))

	// Initially un-notified.
	newPRs, err := s.FindNew(ctx, "reviewer")
	require.NoError(t, err)
	assert.Len(t, newPRs, 1)

	require.NoError(t, s.MarkNotified(ctx, "r1"))

	// After notification, FindNew should return nothing.
	newPRs, err = s.FindNew(ctx, "reviewer")
	require.NoError(t, err)
	assert.Empty(t, newPRs)
}

func TestUpdateActivity_ClearsNotifiedAt_WhenNewer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("a1", "org/repo", 1, "author")))
	require.NoError(t, s.MarkNotified(ctx, "a1"))

	// Verify it's notified and NOT in FindNew.
	newPRs, err := s.FindNew(ctx, "author")
	require.NoError(t, err)
	assert.Empty(t, newPRs)

	// Update with activity newer than notified_at — should clear notified_at.
	futureTime := time.Now().UTC().Add(1 * time.Hour)
	require.NoError(t, s.UpdateActivity(ctx, "a1", futureTime, "approved", "reviewer1"))

	// Should now appear in FindNew again.
	newPRs, err = s.FindNew(ctx, "author")
	require.NoError(t, err)
	assert.Len(t, newPRs, 1)
	assert.Equal(t, "a1", newPRs[0].PRID)
	assert.NotNil(t, newPRs[0].LastActivityAt)
	assert.Equal(t, "approved", *newPRs[0].LastActivityType)
	assert.Equal(t, "reviewer1", *newPRs[0].LastActivityBy)
}

func TestUpdateActivity_PreservesNotifiedAt_WhenOlder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("a1", "org/repo", 1, "author")))
	require.NoError(t, s.MarkNotified(ctx, "a1"))

	// Update with activity that is OLDER than notified_at — should NOT clear notified_at.
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpdateActivity(ctx, "a1", pastTime, "commented", "reviewer2"))

	// Should NOT appear in FindNew (notified_at preserved).
	newPRs, err := s.FindNew(ctx, "author")
	require.NoError(t, err)
	assert.Empty(t, newPRs)

	// But the activity fields should still be updated.
	prs, err := s.GetPendingByRole(ctx, "author")
	require.NoError(t, err)
	require.Len(t, prs, 1)
	assert.Equal(t, "commented", *prs[0].LastActivityType)
	assert.Equal(t, "reviewer2", *prs[0].LastActivityBy)
}

func TestCleanup_RemovesPendingNotInSet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r3", "org/c", 3, "reviewer")))

	// Only r1 is still open on GitHub.
	require.NoError(t, s.Cleanup(ctx, []string{"r1"}))

	prs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	assert.Len(t, prs, 1)
	assert.Equal(t, "r1", prs[0].PRID)
}

func TestCleanup_PreservesDismissedPRs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))

	// Dismiss r2.
	require.NoError(t, s.Dismiss(ctx, "r2"))

	// Cleanup with empty current set — should only remove pending PRs.
	require.NoError(t, s.Cleanup(ctx, []string{}))

	// r1 (pending) should be gone, r2 (dismissed) should remain.
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pull_requests`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var prid string
	err = s.db.QueryRowContext(ctx, `SELECT pr_id FROM pull_requests`).Scan(&prid)
	require.NoError(t, err)
	assert.Equal(t, "r2", prid)
}

func TestGetCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("a1", "org/c", 3, "author")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("a2", "org/d", 4, "author")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("a3", "org/e", 5, "author")))

	// Dismiss one from each role — should not count.
	require.NoError(t, s.Dismiss(ctx, "r1"))
	require.NoError(t, s.Dismiss(ctx, "a1"))

	reviewCount, authoredCount, err := s.GetCounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, reviewCount)
	assert.Equal(t, 2, authoredCount)
}

func TestOldestPendingAge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No PRs — should return 0.
	age, err := s.OldestPendingAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age)

	// Insert a reviewer PR.
	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))

	age, err = s.OldestPendingAge(ctx)
	require.NoError(t, err)
	// first_seen was just set, so age should be very small (< 1 second).
	assert.True(t, age >= 0 && age < 2*time.Second, "expected age near zero, got %v", age)
}

func TestOldestPendingAge_IgnoresNonReviewerAndNonPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Only author PRs — oldest pending reviewer age should be 0.
	require.NoError(t, s.UpsertPR(ctx, samplePR("a1", "org/a", 1, "author")))

	age, err := s.OldestPendingAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age)

	// Add a reviewer PR then dismiss it — should still be 0.
	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/b", 2, "reviewer")))
	require.NoError(t, s.Dismiss(ctx, "r1"))

	age, err = s.OldestPendingAge(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age)
}

func TestUpsertPR_IsDraft_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pr := samplePR("node-draft", "org/repo", 99, "author")
	pr.IsDraft = true
	require.NoError(t, s.UpsertPR(ctx, pr))

	prs, err := s.GetPendingByRole(ctx, "author")
	require.NoError(t, err)
	require.Len(t, prs, 1)
	assert.True(t, prs[0].IsDraft, "draft flag should round-trip through upsert+query")

	// Upsert again with IsDraft=false (PR was marked ready for review).
	pr.IsDraft = false
	require.NoError(t, s.UpsertPR(ctx, pr))

	prs, err = s.GetPendingByRole(ctx, "author")
	require.NoError(t, err)
	require.Len(t, prs, 1)
	assert.False(t, prs[0].IsDraft, "draft flag should update on upsert")
}

func TestUpsertPR_IsDraft_DefaultFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pr := samplePR("node-nondraft", "org/repo", 100, "author")
	// IsDraft defaults to false (zero value).
	require.NoError(t, s.UpsertPR(ctx, pr))

	prs, err := s.GetPendingByRole(ctx, "author")
	require.NoError(t, err)
	require.Len(t, prs, 1)
	assert.False(t, prs[0].IsDraft, "non-draft PR should have IsDraft=false")
}

func TestCleanup_EmptyCurrentIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertPR(ctx, samplePR("r1", "org/a", 1, "reviewer")))
	require.NoError(t, s.UpsertPR(ctx, samplePR("r2", "org/b", 2, "reviewer")))

	// Empty slice means nothing is current — all pending get removed.
	require.NoError(t, s.Cleanup(ctx, []string{}))

	prs, err := s.GetPendingByRole(ctx, "reviewer")
	require.NoError(t, err)
	assert.Empty(t, prs)
}
