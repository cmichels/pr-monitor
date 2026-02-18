package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status represents the data written to the status JSON file.
// Wezterm reads this file on a timer to update the right-status bar badge.
type Status struct {
	ReviewCount           int       `json:"review_count"`
	AuthoredActivityCount int       `json:"authored_activity_count"`
	LastUpdated           time.Time `json:"last_updated"`
	OldestReviewAgeHours  float64   `json:"oldest_review_age_hours"`
}

// WriteStatus atomically writes the status JSON file.
// Uses write-to-temp-then-rename to prevent wezterm from reading partial data.
// Parent directories are created if they don't exist.
func WriteStatus(path string, status Status) error {
	// Expand ~ defensively (caller should do this, but be safe).
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, path[2:])
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Create temp file in the same directory to guarantee same-filesystem rename.
	tmp, err := os.CreateTemp(dir, ".status-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	// Compact JSON — single line, wezterm reads it frequently.
	data, err := json.Marshal(status)
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename — POSIX guarantees this is atomic on the same filesystem.
	return os.Rename(tmpPath, path)
}
