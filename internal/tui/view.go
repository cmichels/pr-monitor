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
			Underline(true).
			Foreground(lipgloss.Color("205")).
			Background(lipgloss.Color("236")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Faint(true).
				Padding(0, 2)

	headerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("236"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("178")).
			Padding(0, 1)

	helpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(1, 2)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Width(16)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
)

// renderView builds the full TUI output from the model state.
func renderView(m Model) string {
	if m.width == 0 {
		return "Initializing..."
	}

	// Help overlay takes over the entire view.
	if m.showHelp {
		return renderHelpOverlay(m)
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

	// Footer: key hints + status.
	b.WriteString(renderFooter())
	if m.statusText != "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.statusText))
	}

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
	return headerStyle.Render(title + "  " + tabBar)
}

// renderFooter renders keybinding hints.
func renderFooter() string {
	return footerStyle.Render("tab: switch | r: review | d: dismiss | o: open | R: refresh | ?: help | q: quit")
}

// renderHelpOverlay renders a full-screen help overlay with all keybindings.
func renderHelpOverlay(m Model) string {
	bindings := []struct{ key, desc string }{
		{"tab / shift+tab", "Switch tabs"},
		{"r / enter", "Launch review (To Review) / Jump to repo (My PRs)"},
		{"d", "Dismiss PR"},
		{"o", "Open PR in browser"},
		{"R", "Force refresh"},
		{"/", "Filter list"},
		{"?", "Toggle this help"},
		{"q / ctrl+c", "Quit"},
	}

	var rows []string
	for _, b := range bindings {
		row := helpKeyStyle.Render(b.key) + helpDescStyle.Render(b.desc)
		rows = append(rows, row)
	}

	content := titleStyle.Render("Keybindings") + "\n\n" + strings.Join(rows, "\n")
	overlay := helpOverlayStyle.Render(content)

	// Center the overlay in the terminal.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}
