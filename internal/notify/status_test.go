package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWriteStatus_CreatesFileWithCorrectJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	ts := time.Date(2026, 2, 18, 14, 30, 0, 0, time.UTC)
	status := Status{
		ReviewCount:           3,
		AuthoredActivityCount: 2,
		LastUpdated:           ts,
		OldestReviewAgeHours:  72.5,
	}

	err := WriteStatus(path, status)
	assert.NoError(t, err)

	data, err := os.ReadFile(path)
	assert.NoError(t, err)

	// Verify it's valid JSON and fields match.
	var got Status
	err = json.Unmarshal(data, &got)
	assert.NoError(t, err)
	assert.Equal(t, 3, got.ReviewCount)
	assert.Equal(t, 2, got.AuthoredActivityCount)
	assert.Equal(t, ts, got.LastUpdated)
	assert.InDelta(t, 72.5, got.OldestReviewAgeHours, 0.001)

	// Verify compact format (single line, no indentation).
	assert.NotContains(t, string(data), "\n")
}

func TestWriteStatus_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	ts := time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC)

	// Write initial.
	err := WriteStatus(path, Status{ReviewCount: 1, LastUpdated: ts})
	assert.NoError(t, err)

	// Overwrite.
	err = WriteStatus(path, Status{ReviewCount: 5, AuthoredActivityCount: 3, LastUpdated: ts, OldestReviewAgeHours: 10.0})
	assert.NoError(t, err)

	data, err := os.ReadFile(path)
	assert.NoError(t, err)

	var got Status
	err = json.Unmarshal(data, &got)
	assert.NoError(t, err)
	assert.Equal(t, 5, got.ReviewCount)
	assert.Equal(t, 3, got.AuthoredActivityCount)
}

func TestWriteStatus_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "status.json")

	err := WriteStatus(path, Status{ReviewCount: 1, LastUpdated: time.Now()})
	assert.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestWriteStatus_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	ts := time.Date(2026, 2, 18, 14, 30, 0, 0, time.UTC)
	status := Status{
		ReviewCount:           7,
		AuthoredActivityCount: 4,
		LastUpdated:           ts,
		OldestReviewAgeHours:  24.0,
	}

	err := WriteStatus(path, status)
	assert.NoError(t, err)

	// After write, the file must be valid JSON (no partial content).
	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.True(t, json.Valid(data), "file content should be valid JSON")

	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Len(t, entries, 1, "only the final status.json should exist, no temp files")
}

func TestWriteStatus_JSONFieldValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	ts := time.Date(2026, 2, 18, 14, 30, 0, 0, time.UTC)
	status := Status{
		ReviewCount:           0,
		AuthoredActivityCount: 0,
		LastUpdated:           ts,
		OldestReviewAgeHours:  0,
	}

	err := WriteStatus(path, status)
	assert.NoError(t, err)

	data, err := os.ReadFile(path)
	assert.NoError(t, err)

	// Verify exact JSON structure with zero values.
	want := `{"review_count":0,"authored_activity_count":0,"last_updated":"2026-02-18T14:30:00Z","oldest_review_age_hours":0}`
	assert.JSONEq(t, want, string(data))
}
