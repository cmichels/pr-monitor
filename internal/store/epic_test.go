package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncTrackedEpics(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	epics := []TrackedEpic{
		{EpicKey: "OP-3309", Name: "UX/UI Design 2026", SortOrder: 0},
		{EpicKey: "OP-4000", Name: "Backend Rewrite", SortOrder: 1},
	}

	// First sync: inserts both.
	err := s.SyncTrackedEpics(ctx, epics)
	require.NoError(t, err)

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "OP-3309", got[0].EpicKey)
	assert.Equal(t, "UX/UI Design 2026", got[0].Name)
	assert.True(t, got[0].Active)
	assert.Equal(t, "OP-4000", got[1].EpicKey)

	// Second sync with updated name: updates name, preserves active.
	epics[0].Name = "UX Design 2026 v2"
	err = s.SyncTrackedEpics(ctx, epics)
	require.NoError(t, err)

	got, err = s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Equal(t, "UX Design 2026 v2", got[0].Name)
}

func TestSyncTrackedEpics_PreservesActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	epics := []TrackedEpic{
		{EpicKey: "OP-100", Name: "Test Epic", SortOrder: 0},
	}
	require.NoError(t, s.SyncTrackedEpics(ctx, epics))

	// Deactivate the epic manually (simulating Phase 3 toggle).
	_, err := s.db.ExecContext(ctx, `UPDATE tracked_epics SET active = 0 WHERE epic_key = 'OP-100'`)
	require.NoError(t, err)

	// Re-sync: should not reactivate.
	require.NoError(t, s.SyncTrackedEpics(ctx, epics))

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 0, "deactivated epic should not reappear in active list")
}

func TestGetActiveTrackedEpics_Ordering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	epics := []TrackedEpic{
		{EpicKey: "OP-300", Name: "C", SortOrder: 2},
		{EpicKey: "OP-100", Name: "A", SortOrder: 0},
		{EpicKey: "OP-200", Name: "B", SortOrder: 1},
	}
	require.NoError(t, s.SyncTrackedEpics(ctx, epics))

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "OP-100", got[0].EpicKey)
	assert.Equal(t, "OP-200", got[1].EpicKey)
	assert.Equal(t, "OP-300", got[2].EpicKey)
}

func TestCleanupJiraIssuesByPrefix(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert some epic issues and some filter issues.
	issues := []JiraIssue{
		{Key: "OP-1", Summary: "epic child 1", Source: "epic:OP-3309", Status: "To Do", StatusCat: "new"},
		{Key: "OP-2", Summary: "epic child 2", Source: "epic:OP-3309", Status: "To Do", StatusCat: "new"},
		{Key: "OP-3", Summary: "filter issue", Source: "filter:123", Status: "To Do", StatusCat: "new"},
		{Key: "OP-4", Summary: "my task", Source: "my_tasks", Status: "To Do", StatusCat: "new"},
	}
	for _, issue := range issues {
		require.NoError(t, s.UpsertJiraIssue(ctx, issue))
	}

	// Cleanup epic issues, keeping only OP-1.
	err := s.CleanupJiraIssuesByPrefix(ctx, "epic:", []string{"OP-1"})
	require.NoError(t, err)

	// OP-2 should be deleted, but OP-3 and OP-4 should remain.
	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3, "should have 3 issues remaining")

	keys := make(map[string]bool)
	for _, issue := range all {
		keys[issue.Key] = true
	}
	assert.True(t, keys["OP-1"], "OP-1 should remain (in currentKeys)")
	assert.False(t, keys["OP-2"], "OP-2 should be deleted (epic, not in currentKeys)")
	assert.True(t, keys["OP-3"], "OP-3 should remain (not an epic)")
	assert.True(t, keys["OP-4"], "OP-4 should remain (not an epic)")
}

func TestCleanupJiraIssuesByPrefix_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertJiraIssue(ctx, JiraIssue{Key: "OP-1", Source: "epic:OP-100", Status: "To Do", StatusCat: "new"}))
	require.NoError(t, s.UpsertJiraIssue(ctx, JiraIssue{Key: "OP-2", Source: "filter:123", Status: "To Do", StatusCat: "new"}))

	// Empty currentKeys: deletes all epic issues.
	err := s.CleanupJiraIssuesByPrefix(ctx, "epic:", nil)
	require.NoError(t, err)

	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, "OP-2", all[0].Key)
}

func TestAddTrackedEpic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Add a new epic.
	err := s.AddTrackedEpic(ctx, "OP-5000", "New Feature")
	require.NoError(t, err)

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "OP-5000", got[0].EpicKey)
	assert.Equal(t, "New Feature", got[0].Name)
	assert.True(t, got[0].Active)
}

func TestAddTrackedEpic_ReactivatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed and deactivate.
	require.NoError(t, s.SyncTrackedEpics(ctx, []TrackedEpic{
		{EpicKey: "OP-100", Name: "Old Name", SortOrder: 0},
	}))
	require.NoError(t, s.SetEpicActive(ctx, "OP-100", false))

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 0)

	// Re-add should reactivate with updated name.
	require.NoError(t, s.AddTrackedEpic(ctx, "OP-100", "Updated Name"))

	got, err = s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Updated Name", got[0].Name)
	assert.True(t, got[0].Active)
}

func TestSetEpicActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SyncTrackedEpics(ctx, []TrackedEpic{
		{EpicKey: "OP-100", Name: "Test", SortOrder: 0},
	}))

	// Deactivate.
	require.NoError(t, s.SetEpicActive(ctx, "OP-100", false))
	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 0)

	// Reactivate.
	require.NoError(t, s.SetEpicActive(ctx, "OP-100", true))
	got, err = s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "OP-100", got[0].EpicKey)
}

func TestRemoveTrackedEpic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed an epic and add some child issues.
	require.NoError(t, s.SyncTrackedEpics(ctx, []TrackedEpic{
		{EpicKey: "OP-100", Name: "Doomed Epic", SortOrder: 0},
	}))
	require.NoError(t, s.UpsertJiraIssue(ctx, JiraIssue{
		Key: "OP-10", Summary: "child", Source: "epic:OP-100", Status: "To Do", StatusCat: "new",
	}))
	require.NoError(t, s.UpsertJiraIssue(ctx, JiraIssue{
		Key: "OP-20", Summary: "other filter", Source: "filter:123", Status: "To Do", StatusCat: "new",
	}))

	// Remove the epic.
	err := s.RemoveTrackedEpic(ctx, "OP-100")
	require.NoError(t, err)

	// Epic should be gone.
	epics, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	assert.Len(t, epics, 0)

	// Child issues should be gone, but other issues should remain.
	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, "OP-20", all[0].Key)
}

func TestAddTrackedEpic_SortOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Add epics one at a time — each should get incrementing sort_order.
	require.NoError(t, s.AddTrackedEpic(ctx, "OP-1", "First"))
	require.NoError(t, s.AddTrackedEpic(ctx, "OP-2", "Second"))
	require.NoError(t, s.AddTrackedEpic(ctx, "OP-3", "Third"))

	got, err := s.GetActiveTrackedEpics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "OP-1", got[0].EpicKey)
	assert.Equal(t, "OP-2", got[1].EpicKey)
	assert.Equal(t, "OP-3", got[2].EpicKey)
}

func TestCleanupJiraIssuesNonEpic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issues := []JiraIssue{
		{Key: "OP-1", Summary: "epic child", Source: "epic:OP-3309", Status: "To Do", StatusCat: "new"},
		{Key: "OP-2", Summary: "filter issue", Source: "filter:123", Status: "To Do", StatusCat: "new"},
		{Key: "OP-3", Summary: "my task", Source: "my_tasks", Status: "To Do", StatusCat: "new"},
	}
	for _, issue := range issues {
		require.NoError(t, s.UpsertJiraIssue(ctx, issue))
	}

	// Cleanup non-epic issues, keeping only OP-2.
	err := s.CleanupJiraIssuesNonEpic(ctx, []string{"OP-2"})
	require.NoError(t, err)

	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2, "epic child + OP-2 should remain")

	keys := make(map[string]bool)
	for _, issue := range all {
		keys[issue.Key] = true
	}
	assert.True(t, keys["OP-1"], "OP-1 should remain (it's an epic, not touched)")
	assert.True(t, keys["OP-2"], "OP-2 should remain (in currentKeys)")
	assert.False(t, keys["OP-3"], "OP-3 should be deleted (non-epic, not in currentKeys)")
}

func TestCleanupJiraIssuesNonEpic_PreservesSprintIssues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issues := []JiraIssue{
		{Key: "OP-1", Summary: "epic child", Source: "epic:OP-3309", Status: "To Do", StatusCat: "new"},
		{Key: "OP-2", Summary: "sprint issue", Source: "sprint:Sprint 24", Status: "In Progress", StatusCat: "indeterminate"},
		{Key: "OP-3", Summary: "filter issue", Source: "filter:123", Status: "To Do", StatusCat: "new"},
		{Key: "OP-4", Summary: "my task", Source: "my_tasks", Status: "To Do", StatusCat: "new"},
	}
	for _, issue := range issues {
		require.NoError(t, s.UpsertJiraIssue(ctx, issue))
	}

	// Cleanup with only OP-3 in currentKeys — sprint and epic issues must survive.
	err := s.CleanupJiraIssuesNonEpic(ctx, []string{"OP-3"})
	require.NoError(t, err)

	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3, "epic + sprint + OP-3 should remain")

	keys := make(map[string]bool)
	for _, issue := range all {
		keys[issue.Key] = true
	}
	assert.True(t, keys["OP-1"], "epic child should survive")
	assert.True(t, keys["OP-2"], "sprint issue should survive")
	assert.True(t, keys["OP-3"], "filter issue in currentKeys should survive")
	assert.False(t, keys["OP-4"], "my_task not in currentKeys should be deleted")
}

func TestCleanupJiraIssuesNonEpic_EmptyKeys_PreservesSprintAndEpic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	issues := []JiraIssue{
		{Key: "OP-1", Summary: "epic child", Source: "epic:OP-100", Status: "To Do", StatusCat: "new"},
		{Key: "OP-2", Summary: "sprint issue", Source: "sprint:Sprint 24", Status: "To Do", StatusCat: "new"},
		{Key: "OP-3", Summary: "filter issue", Source: "filter:123", Status: "To Do", StatusCat: "new"},
	}
	for _, issue := range issues {
		require.NoError(t, s.UpsertJiraIssue(ctx, issue))
	}

	// Empty currentKeys: should nuke filter issues but leave epic + sprint.
	err := s.CleanupJiraIssuesNonEpic(ctx, nil)
	require.NoError(t, err)

	all, err := s.GetAllJiraIssues(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2, "only epic + sprint should remain")

	keys := make(map[string]bool)
	for _, issue := range all {
		keys[issue.Key] = true
	}
	assert.True(t, keys["OP-1"], "epic should survive")
	assert.True(t, keys["OP-2"], "sprint should survive")
	assert.False(t, keys["OP-3"], "filter issue should be deleted")
}
