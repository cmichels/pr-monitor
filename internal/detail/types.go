package detail

import "time"

// PRDetail contains on-demand detail for a selected PR.
type PRDetail struct {
	Body     string
	Files    []FileChange
	Checks   []Check
	Comments []Comment
	Reviews  []ReviewStatus
}

// FileChange represents a single changed file in a PR.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
}

// Check represents a CI check or status context on a PR.
type Check struct {
	Name       string
	Status     string
	Conclusion string
}

// Comment represents a PR comment or review.
type Comment struct {
	Author      string
	Body        string
	CreatedAt   time.Time
	ReviewState string
}

// ReviewStatus represents the latest review state for a single reviewer.
type ReviewStatus struct {
	Author string
	State  string
}
