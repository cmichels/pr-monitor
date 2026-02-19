package tui

import (
	"fmt"
	"time"
)

// PRItem implements the bubbles list.DefaultItem interface for displaying PRs.
type PRItem struct {
	pr    PR
	shame ShameConfig
}

// NewPRItem creates a PRItem from a PR with shame timer config.
func NewPRItem(pr PR, shame ShameConfig) PRItem {
	return PRItem{pr: pr, shame: shame}
}

func (i PRItem) FilterValue() string { return i.pr.Title }

func (i PRItem) Title() string {
	return fmt.Sprintf("%s #%d  %s", i.pr.Repo, i.pr.Number, i.pr.Title)
}

func (i PRItem) Description() string {
	age := time.Since(i.pr.FirstSeen)
	ageText := FormatAge(age)
	styledAge := AgeStyle(age, i.shame).Render(ageText)

	parts := fmt.Sprintf("by @%s | %d files | CI %s | %s",
		i.pr.Author,
		i.pr.FilesChanged,
		StyledCI(i.pr.CIStatus),
		styledAge,
	)

	if i.pr.ReviewerStatus != "" {
		parts += " | " + StyledReviewerStatus(i.pr.ReviewerStatus)
	}

	if i.pr.LastActivityType != "" && i.pr.LastActivityBy != "" {
		parts += fmt.Sprintf(" | %s by @%s",
			StyledActivity(i.pr.LastActivityType),
			i.pr.LastActivityBy,
		)
	}

	return parts
}
