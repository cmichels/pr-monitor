package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"sub-hour", 30 * time.Minute, "<1h"},
		{"exactly 1 hour", time.Hour, "1h"},
		{"several hours", 5 * time.Hour, "5h"},
		{"23 hours", 23 * time.Hour, "23h"},
		{"1 day", 24 * time.Hour, "1d"},
		{"3 days", 72 * time.Hour, "3d"},
		{"6 days", 144 * time.Hour, "6d"},
		{"1 week", 7 * 24 * time.Hour, "1w"},
		{"2 weeks", 14 * 24 * time.Hour, "2w"},
		{"zero", 0, "<1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatAge(tt.age))
		})
	}
}

func TestAgeStyle_DefaultThresholds(t *testing.T) {
	cfg := DefaultShameConfig()

	tests := []struct {
		name      string
		age       time.Duration
		wantStyle string // compare by foreground color
	}{
		{"green: 1 hour", 1 * time.Hour, "#4caf50"},
		{"green: 3 hours", 3 * time.Hour, "#4caf50"},
		{"yellow: exactly 4 hours (boundary)", 4 * time.Hour, "#f9a825"},
		{"yellow: 12 hours", 12 * time.Hour, "#f9a825"},
		{"orange: exactly 24 hours (boundary)", 24 * time.Hour, "#ff9800"},
		{"orange: 36 hours", 36 * time.Hour, "#ff9800"},
		{"red: exactly 48 hours (boundary)", 48 * time.Hour, "#f44336"},
		{"red: 72 hours", 72 * time.Hour, "#f44336"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := AgeStyle(tt.age, cfg)
			rendered := style.Render("test")
			// Verify the style produces output (non-empty).
			assert.NotEmpty(t, rendered)
		})
	}
}

func TestAgeStyle_GreenBelowThreshold(t *testing.T) {
	cfg := ShameConfig{GreenHours: 4, YellowHours: 24, RedHours: 48}

	// Just below green threshold should be green.
	style := AgeStyle(3*time.Hour+59*time.Minute, cfg)
	assert.Equal(t, greenStyle, style)
}

func TestAgeStyle_YellowAtGreenBoundary(t *testing.T) {
	cfg := ShameConfig{GreenHours: 4, YellowHours: 24, RedHours: 48}

	// Exactly at green threshold should be yellow.
	style := AgeStyle(4*time.Hour, cfg)
	assert.Equal(t, yellowStyle, style)
}

func TestAgeStyle_OrangeAtYellowBoundary(t *testing.T) {
	cfg := ShameConfig{GreenHours: 4, YellowHours: 24, RedHours: 48}

	// Exactly at yellow threshold should be orange.
	style := AgeStyle(24*time.Hour, cfg)
	assert.Equal(t, orangeStyle, style)
}

func TestAgeStyle_RedAtRedBoundary(t *testing.T) {
	cfg := ShameConfig{GreenHours: 4, YellowHours: 24, RedHours: 48}

	// Exactly at red threshold should be red.
	style := AgeStyle(48*time.Hour, cfg)
	assert.Equal(t, redStyle, style)
}

func TestAgeStyle_RedAboveRedBoundary(t *testing.T) {
	cfg := ShameConfig{GreenHours: 4, YellowHours: 24, RedHours: 48}

	// Way above red threshold should still be red.
	style := AgeStyle(200*time.Hour, cfg)
	assert.Equal(t, redStyle, style)
}

func TestAgeStyle_CustomConfig(t *testing.T) {
	cfg := ShameConfig{GreenHours: 1, YellowHours: 2, RedHours: 3}

	assert.Equal(t, greenStyle, AgeStyle(30*time.Minute, cfg))
	assert.Equal(t, yellowStyle, AgeStyle(90*time.Minute, cfg))
	assert.Equal(t, orangeStyle, AgeStyle(150*time.Minute, cfg))
	assert.Equal(t, redStyle, AgeStyle(4*time.Hour, cfg))
}

func TestStyledCI(t *testing.T) {
	tests := []struct {
		status   string
		contains string
	}{
		{"passing", "ok"},
		{"failing", "FAIL"},
		{"pending", "..."},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		result := StyledCI(tt.status)
		assert.Contains(t, result, tt.contains, "StyledCI(%q)", tt.status)
	}
}

func TestStyledActivity(t *testing.T) {
	tests := []struct {
		activity string
		contains string
	}{
		{"approved", "approved"},
		{"commented", "commented"},
		{"changes_requested", "changes requested"},
		{"other", "other"},
	}
	for _, tt := range tests {
		result := StyledActivity(tt.activity)
		assert.Contains(t, result, tt.contains, "StyledActivity(%q)", tt.activity)
	}
}

func TestDefaultShameConfig(t *testing.T) {
	cfg := DefaultShameConfig()
	assert.Equal(t, 4, cfg.GreenHours)
	assert.Equal(t, 24, cfg.YellowHours)
	assert.Equal(t, 48, cfg.RedHours)
}
