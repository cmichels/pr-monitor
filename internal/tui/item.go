package tui

import (
	"fmt"
	"time"
)

// PRItem implements the bubbles list.DefaultItem interface for displaying PRs.
type PRItem struct {
	pr PR
}

// NewPRItem creates a PRItem from a PR.
func NewPRItem(pr PR) PRItem {
	return PRItem{pr: pr}
}

func (i PRItem) FilterValue() string { return i.pr.Title }

func (i PRItem) Title() string {
	return fmt.Sprintf("%s #%d  %s", i.pr.Repo, i.pr.Number, i.pr.Title)
}

func (i PRItem) Description() string {
	parts := fmt.Sprintf("by @%s | %d files | CI %s | %s",
		i.pr.Author,
		i.pr.FilesChanged,
		ciSymbol(i.pr.CIStatus),
		relativeAge(i.pr.FirstSeen),
	)

	if i.pr.LastActivityType != "" && i.pr.LastActivityBy != "" {
		parts += fmt.Sprintf(" | %s by @%s", i.pr.LastActivityType, i.pr.LastActivityBy)
	}

	return parts
}

// ciSymbol returns a compact symbol for CI status.
func ciSymbol(status string) string {
	switch status {
	case "passing":
		return "ok"
	case "failing":
		return "FAIL"
	case "pending":
		return "..."
	default:
		return "?"
	}
}

// relativeAge formats a time as a human-readable relative age.
func relativeAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
