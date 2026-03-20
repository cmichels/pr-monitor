package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// renderSprintTab renders the Sprint tab content.
func renderSprintTab(m Model) string {
	if m.sprintLoader == nil {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true).
			Padding(1, 2)
		return emptyStyle.Render("Sprint not configured")
	}

	if len(m.sprintPartition.mine) == 0 && len(m.sprintPartition.unassigned) == 0 && len(m.sprintPartition.others) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true).
			Padding(1, 2)
		return emptyStyle.Render("No sprint issues found")
	}

	var parts []string

	// Sprint header with progress bar.
	header := renderSprintHeader(m.sprintName, m.sprintStats)
	parts = append(parts, header)

	// List view.
	parts = append(parts, m.sprintList.View())

	// Others hint line.
	othersCount := len(m.sprintPartition.others)
	if othersCount > 0 {
		othersHintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Faint(true).
			Padding(0, 3)

		if !m.sprintShowOthers {
			hint := fmt.Sprintf("  %d assigned to others [e to show]", othersCount)
			parts = append(parts, othersHintStyle.Render(hint))
		} else {
			hint := fmt.Sprintf("  showing all · %d others [e to hide]", othersCount)
			parts = append(parts, othersHintStyle.Render(hint))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderSprintHeader renders the sprint name with progress bar and stats.
func renderSprintHeader(name string, stats SprintStats) string {
	label := name
	if label == "" {
		label = "Sprint"
	}

	if stats.Total == 0 {
		return sectionFocusedStyle.Render(label + " (0)")
	}

	bar := renderProgressBar(stats.Done, stats.Total, 12)
	pct := stats.Done * 100 / stats.Total

	suffix := fmt.Sprintf(" %s %d%% · %d/%d done", bar, pct, stats.Done, stats.Total)
	if stats.Mine > 0 {
		suffix += fmt.Sprintf(" · %d mine", stats.Mine)
	}
	if stats.UpForGrabs > 0 {
		suffix += fmt.Sprintf(" · %d up for grabs", stats.UpForGrabs)
	}
	if stats.Blocked > 0 {
		suffix += fmt.Sprintf(" · %d blocked", stats.Blocked)
	}

	return sectionFocusedStyle.Render(label + suffix)
}

// resizeSprintSection recalculates the sprint list size.
func (m *Model) resizeSprintSection() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	// Reserve 1 line for sprint header + 1 for others hint if applicable.
	overhead := 1
	if len(m.sprintPartition.others) > 0 {
		overhead++
	}
	listHeight -= overhead
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.jiraDetailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	m.sprintList.SetSize(listWidth, listHeight)
}
