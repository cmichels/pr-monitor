package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	settingsCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)

	settingsNameStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Bold(true).
				Width(24)

	settingsDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
)

// renderSettingsTab renders the Settings tab action list.
func renderSettingsTab(m Model) string {
	if len(m.settingsActions) == 0 {
		return loadingStyle.Render("No actions available")
	}

	var b strings.Builder
	b.WriteString("\n")

	for i, action := range m.settingsActions {
		cursor := "  "
		nameStyle := settingsNameStyle
		if i == m.settingsCursor {
			cursor = settingsCursorStyle.Render("> ")
			nameStyle = nameStyle.Foreground(lipgloss.Color("205"))
		}

		line := fmt.Sprintf("%s%s%s",
			cursor,
			nameStyle.Render(action.name),
			settingsDescStyle.Render(action.desc),
		)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Pad to fill available height so the layout doesn't collapse.
	lines := len(m.settingsActions) + 1 // +1 for the leading newline
	availHeight := m.height - 5
	for i := lines; i < availHeight; i++ {
		b.WriteString("\n")
	}

	return b.String()
}
