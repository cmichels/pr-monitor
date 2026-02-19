package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ShameConfig defines the hour thresholds for color-coded PR age.
// PRs older than each threshold get progressively more alarming colors.
type ShameConfig struct {
	GreenHours  int
	YellowHours int
	RedHours    int
}

// DefaultShameConfig returns the default shame timer thresholds.
func DefaultShameConfig() ShameConfig {
	return ShameConfig{
		GreenHours:  4,
		YellowHours: 24,
		RedHours:    48,
	}
}

// Shame timer color styles.
var (
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	orangeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9800"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f44336"))
)

// CI status styles.
var (
	ciPassStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	ciFailStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f44336"))
	ciPendStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	ciUnknownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// Activity type styles.
var (
	approvedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	commentedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	changesStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9800"))
)

// AgeStyle returns the appropriate lipgloss style for a PR's age
// based on the shame timer thresholds.
func AgeStyle(age time.Duration, cfg ShameConfig) lipgloss.Style {
	hours := age.Hours()
	switch {
	case hours < float64(cfg.GreenHours):
		return greenStyle
	case hours < float64(cfg.YellowHours):
		return yellowStyle
	case hours < float64(cfg.RedHours):
		return orangeStyle
	default:
		return redStyle
	}
}

// FormatAge returns a human-readable age string.
func FormatAge(age time.Duration) string {
	hours := age.Hours()
	switch {
	case hours < 1:
		return "<1h"
	case hours < 24:
		return fmt.Sprintf("%dh", int(hours))
	case hours < 24*7:
		return fmt.Sprintf("%dd", int(hours/24))
	default:
		return fmt.Sprintf("%dw", int(hours/(24*7)))
	}
}

// StyledCI returns a CI status indicator with color.
func StyledCI(status string) string {
	switch status {
	case "passing":
		return ciPassStyle.Render("ok")
	case "failing":
		return ciFailStyle.Render("FAIL")
	case "pending":
		return ciPendStyle.Render("...")
	default:
		return ciUnknownStyle.Render("?")
	}
}

// Section header styles for stacked Pending/Reviewed sections.
var (
	sectionFocusedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
	sectionDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Faint(true).Padding(0, 1)
)

// Reviewer status styles.
var (
	reviewerPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Faint(true)
)

// StyledReviewerStatus returns a styled reviewer status badge.
func StyledReviewerStatus(status string) string {
	switch status {
	case "approved":
		return approvedStyle.Render("[approved]")
	case "changes_requested":
		return changesStyle.Render("[changes requested]")
	case "commented":
		return commentedStyle.Render("[commented]")
	default:
		return reviewerPendingStyle.Render("[pending]")
	}
}

// StyledActivity returns an activity type string with color.
func StyledActivity(activityType string) string {
	switch activityType {
	case "approved":
		return approvedStyle.Render("approved")
	case "commented":
		return commentedStyle.Render("commented")
	case "changes_requested":
		return changesStyle.Render("changes requested")
	default:
		return activityType
	}
}
