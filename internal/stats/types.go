package stats

// Progress reports backfill progress for a single fetch cycle.
type Progress struct {
	Done    int
	Total   int
	Current string // e.g. "fetching @username"
}

// ProgressFunc is called during backfill to report progress.
type ProgressFunc func(Progress)
