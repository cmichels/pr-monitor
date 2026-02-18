package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Background(lipgloss.Color("236")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 2)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)
)

// renderView builds the full TUI output from the model state.
func renderView(m Model) string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// Header: title + tab bar.
	b.WriteString(renderHeader(m))
	b.WriteString("\n")

	// Error banner, if any.
	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	}

	// Body: active list.
	b.WriteString(m.lists[m.activeTab].View())
	b.WriteString("\n")

	// Footer: key hints.
	b.WriteString(renderFooter())

	return b.String()
}

// renderHeader renders the title and tab bar.
func renderHeader(m Model) string {
	title := titleStyle.Render("PR Monitor")

	var tabs []string
	for i, tab := range m.tabs {
		count := len(m.lists[i].Items())
		label := fmt.Sprintf("%s (%d)", tab, count)
		if i == m.activeTab {
			tabs = append(tabs, activeTabStyle.Render(label))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(label))
		}
	}

	tabBar := strings.Join(tabs, " ")
	return title + "  " + tabBar
}

// renderFooter renders keybinding hints.
func renderFooter() string {
	return footerStyle.Render("tab: switch tab | /: filter | q: quit")
}
