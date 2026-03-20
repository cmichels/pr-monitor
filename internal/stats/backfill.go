package stats

import (
	"context"
	"fmt"
	"time"

	"github.com/chrismichels/pr-monitor/internal/store"
)

// BackfillNeeded checks whether a full stats fetch is needed.
// Returns true if no fetch has been done, the last fetch was more than 24h ago,
// or the fetch timestamp exists but the stats table is empty (indicates a failed backfill).
func BackfillNeeded(ctx context.Context, s *store.Store) (bool, error) {
	val, ok, err := s.GetMeta(ctx, "last_full_fetch")
	if err != nil {
		return false, fmt.Errorf("check backfill: %w", err)
	}
	if !ok {
		return true, nil
	}
	lastFetch, err := time.Parse("2006-01-02", val)
	if err != nil {
		return true, nil
	}
	if time.Since(lastFetch) > 24*time.Hour {
		return true, nil
	}
	// Recent fetch timestamp exists — but verify data is actually present.
	// A missing read:org scope or wrong team slug can silently produce an empty
	// backfill that marks last_full_fetch without writing any rows.
	hasData, err := s.HasStats(ctx)
	if err != nil {
		return false, err
	}
	return !hasData, nil
}

// RunBackfill fetches stats for all team members and persists them.
func RunBackfill(ctx context.Context, fetcher *Fetcher, s *store.Store, teams []string, onProgress ProgressFunc) error {
	// Fetch team members.
	members, err := fetcher.FetchTeamMembers(ctx, teams)
	if err != nil {
		return fmt.Errorf("fetch team members: %w", err)
	}
	if len(teams) > 0 && len(members) == 0 {
		return fmt.Errorf("no team members found for teams %v — verify the GitHub token has read:org scope (gh auth refresh -s read:org) and org/team slugs are correct", teams)
	}

	// Cache team members list.
	if err := s.SetTeamMembers(ctx, members); err != nil {
		return fmt.Errorf("cache team members: %w", err)
	}

	total := len(members)
	for i, login := range members {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if onProgress != nil {
			onProgress(Progress{Done: i, Total: total, Current: fmt.Sprintf("fetching @%s", login)})
		}

		stats, err := fetcher.FetchUserStats(ctx, login)
		if err != nil {
			return fmt.Errorf("fetch stats for %s: %w", login, err)
		}

		for _, stat := range stats {
			if err := s.UpsertDayStat(ctx, stat); err != nil {
				return fmt.Errorf("upsert stat for %s: %w", login, err)
			}
		}
	}

	// Mark the fetch as complete.
	today := time.Now().Format("2006-01-02")
	if err := s.SetMeta(ctx, "last_full_fetch", today); err != nil {
		return fmt.Errorf("set last_full_fetch: %w", err)
	}

	if onProgress != nil {
		onProgress(Progress{Done: total, Total: total, Current: "done"})
	}

	return nil
}
