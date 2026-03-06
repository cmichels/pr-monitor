package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleDayStat(login, date string) DailyStat {
	return DailyStat{
		Login:            login,
		Date:             date,
		PRsCreated:       2,
		PRsMerged:        1,
		PRsReviewed:      3,
		CommentsGiven:    5,
		LinesAdded:       100,
		LinesRemoved:     50,
		ApprovalsGiven:   2,
		ChangesRequested: 1,
	}
}

func TestUpsertDayStat_InsertAndUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	stat := sampleDayStat("alice", "2026-01-15")
	require.NoError(t, s.UpsertDayStat(ctx, stat))

	stats, err := s.GetStatsByLogin(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 2, stats[0].PRsCreated)
	assert.Equal(t, 100, stats[0].LinesAdded)

	// Update same login+date — values should be replaced.
	stat.PRsCreated = 5
	stat.LinesAdded = 200
	require.NoError(t, s.UpsertDayStat(ctx, stat))

	stats, err = s.GetStatsByLogin(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 5, stats[0].PRsCreated)
	assert.Equal(t, 200, stats[0].LinesAdded)
}

func TestGetAllStats_OrderedByDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("alice", "2026-01-20")))
	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("bob", "2026-01-10")))
	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("alice", "2026-01-10")))

	stats, err := s.GetAllStats(ctx)
	require.NoError(t, err)
	require.Len(t, stats, 3)
	assert.Equal(t, "2026-01-10", stats[0].Date)
	assert.Equal(t, "alice", stats[0].Login)
	assert.Equal(t, "2026-01-10", stats[1].Date)
	assert.Equal(t, "bob", stats[1].Login)
	assert.Equal(t, "2026-01-20", stats[2].Date)
}

func TestGetTopReviewers_OrderedByReviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Alice: 3+4=7 reviews, Bob: 10 reviews
	require.NoError(t, s.UpsertDayStat(ctx, DailyStat{Login: "alice", Date: "2026-01-10", PRsReviewed: 3}))
	require.NoError(t, s.UpsertDayStat(ctx, DailyStat{Login: "alice", Date: "2026-01-11", PRsReviewed: 4}))
	require.NoError(t, s.UpsertDayStat(ctx, DailyStat{Login: "bob", Date: "2026-01-10", PRsReviewed: 10}))

	top, err := s.GetTopReviewers(ctx, 2)
	require.NoError(t, err)
	require.Len(t, top, 2)
	assert.Equal(t, "bob", top[0].Login)
	assert.Equal(t, 10, top[0].PRsReviewed)
	assert.Equal(t, "alice", top[1].Login)
	assert.Equal(t, 7, top[1].PRsReviewed)
}

func TestGetTopAuthors_OrderedByMerged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertDayStat(ctx, DailyStat{Login: "alice", Date: "2026-01-10", PRsMerged: 8}))
	require.NoError(t, s.UpsertDayStat(ctx, DailyStat{Login: "bob", Date: "2026-01-10", PRsMerged: 12}))

	top, err := s.GetTopAuthors(ctx, 1)
	require.NoError(t, err)
	require.Len(t, top, 1)
	assert.Equal(t, "bob", top[0].Login)
	assert.Equal(t, 12, top[0].PRsMerged)
}

func TestGetMeta_SetMeta_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Key doesn't exist yet.
	_, ok, err := s.GetMeta(ctx, "test_key")
	require.NoError(t, err)
	assert.False(t, ok)

	// Set and read back.
	require.NoError(t, s.SetMeta(ctx, "test_key", "test_value"))
	val, ok, err := s.GetMeta(ctx, "test_key")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "test_value", val)

	// Overwrite.
	require.NoError(t, s.SetMeta(ctx, "test_key", "updated"))
	val, ok, err = s.GetMeta(ctx, "test_key")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "updated", val)
}

func TestTeamMembers_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No members yet.
	members, err := s.GetTeamMembers(ctx)
	require.NoError(t, err)
	assert.Nil(t, members)

	// Set and read back.
	require.NoError(t, s.SetTeamMembers(ctx, []string{"alice", "bob", "charlie"}))
	members, err = s.GetTeamMembers(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "bob", "charlie"}, members)
}

func TestReplaceJiraUserStats_InsertAndReplace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert initial stats.
	initial := []JiraUserStat{
		{DisplayName: "Alice", Period: "ytd", CreatedCount: 5, FinishedCount: 3},
		{DisplayName: "Bob", Period: "ytd", CreatedCount: 2, FinishedCount: 1},
	}
	require.NoError(t, s.ReplaceJiraUserStats(ctx, "ytd", initial))

	got, err := s.GetJiraUserStats(ctx, "ytd")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Alice", got[0].DisplayName) // 5+3=8 > 2+1=3
	assert.Equal(t, 5, got[0].CreatedCount)
	assert.Equal(t, 3, got[0].FinishedCount)

	// Full replace: Alice removed, Charlie added.
	replacement := []JiraUserStat{
		{DisplayName: "Bob", Period: "ytd", CreatedCount: 10, FinishedCount: 8},
		{DisplayName: "Charlie", Period: "ytd", CreatedCount: 1, FinishedCount: 1},
	}
	require.NoError(t, s.ReplaceJiraUserStats(ctx, "ytd", replacement))

	got, err = s.GetJiraUserStats(ctx, "ytd")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Bob", got[0].DisplayName)
	assert.Equal(t, 10, got[0].CreatedCount)
	assert.Equal(t, 8, got[0].FinishedCount)
	assert.Equal(t, "Charlie", got[1].DisplayName)
}

func TestGetJiraUserStats_OrderByTotalDesc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	stats := []JiraUserStat{
		{DisplayName: "Low", Period: "month", CreatedCount: 1, FinishedCount: 0},
		{DisplayName: "High", Period: "month", CreatedCount: 5, FinishedCount: 10},
		{DisplayName: "Mid", Period: "month", CreatedCount: 3, FinishedCount: 2},
	}
	require.NoError(t, s.ReplaceJiraUserStats(ctx, "month", stats))

	got, err := s.GetJiraUserStats(ctx, "month")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "High", got[0].DisplayName) // 15
	assert.Equal(t, "Mid", got[1].DisplayName)  // 5
	assert.Equal(t, "Low", got[2].DisplayName)  // 1
}

func TestGetJiraUserStats_PeriodsIsolated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceJiraUserStats(ctx, "ytd", []JiraUserStat{
		{DisplayName: "Alice", Period: "ytd", CreatedCount: 10, FinishedCount: 5},
	}))
	require.NoError(t, s.ReplaceJiraUserStats(ctx, "month", []JiraUserStat{
		{DisplayName: "Bob", Period: "month", CreatedCount: 2, FinishedCount: 1},
	}))

	ytd, err := s.GetJiraUserStats(ctx, "ytd")
	require.NoError(t, err)
	require.Len(t, ytd, 1)
	assert.Equal(t, "Alice", ytd[0].DisplayName)

	month, err := s.GetJiraUserStats(ctx, "month")
	require.NoError(t, err)
	require.Len(t, month, 1)
	assert.Equal(t, "Bob", month[0].DisplayName)
}

func TestGetFetchedLogins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty initially.
	logins, err := s.GetFetchedLogins(ctx)
	require.NoError(t, err)
	assert.Empty(t, logins)

	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("charlie", "2026-01-10")))
	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("alice", "2026-01-10")))
	require.NoError(t, s.UpsertDayStat(ctx, sampleDayStat("alice", "2026-01-11")))

	logins, err = s.GetFetchedLogins(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "charlie"}, logins)
}
