package poller

import "time"

// PollResult represents a single PR from a GitHub poll cycle.
// Used by both review-request and authored-PR polling.
type PollResult struct {
	PRID         string // GitHub node ID
	Repo         string // "org/repo"
	Number       int
	Title        string
	Author       string
	URL          string
	FilesChanged int
	CIStatus       string // "passing", "failing", "pending", "unknown"
	Role           string // "reviewer" or "author"
	ReviewerStatus string // "pending", "approved", "commented", "changes_requested" (reviewer PRs only)

	// Only for authored PRs (Role == "author")
	LastActivityAt   *time.Time
	LastActivityType *string
	LastActivityBy   *string
}
