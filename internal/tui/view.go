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

// separatorStyle for the thin vertical divider between panels.
var separatorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("236"))

// separatorFocusedStyle highlights the divider when detail panel has focus.
var separatorFocusedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("205"))

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

	// Error banner, if any (poll errors or data load errors).
	if m.errorText != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		b.WriteString(errStyle.Render(m.errorText))
		b.WriteString("\n")
	} else if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	}

	// Body: split layout, full-width, or stats viewport.
	if m.activeTab == 2 {
		// Stats tab: full-width viewport, no detail panel.
		b.WriteString(m.statsViewport.View())
		b.WriteString("\n")
	} else {
		var listView string
		switch m.activeTab {
		case 0:
			listView = renderStackedSections(m)
		case 1:
			listView = renderMyPRsSections(m)
		default:
			listView = m.lists[2].View()
		}

		if m.detailFetcher != nil && m.width >= 80 && m.detailReady {
			separator := separatorView(m.height-5, m.detailFocused)
			detailView := m.detailViewport.View()
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, listView, separator, detailView))
		} else {
			b.WriteString(listView)
		}
		b.WriteString("\n")
	}

	// Footer: key hints + status.
	b.WriteString(renderFooter(m))
	if m.statusText != "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.statusText))
	}

	return b.String()
}

// separatorView renders a thin vertical divider.
// When focused is true, the separator is highlighted to indicate detail panel focus.
func separatorView(height int, focused bool) string {
	if height < 1 {
		height = 1
	}
	style := separatorStyle
	ch := "|"
	if focused {
		style = separatorFocusedStyle
		ch = "┃"
	}
	var lines []string
	for i := 0; i < height; i++ {
		lines = append(lines, style.Render(ch))
	}
	return strings.Join(lines, "\n")
}

// renderStackedSections renders the Pending + Reviewed stacked sections for tab 0.
func renderStackedSections(m Model) string {
	pendingHeader := renderSectionHeader("Pending", len(m.lists[0].Items()), m.reviewSection == 0, m.pendingCollapsed)
	reviewedHeader := renderSectionHeader("Reviewed", len(m.lists[1].Items()), m.reviewSection == 1, m.reviewedCollapsed)

	var parts []string

	parts = append(parts, pendingHeader)
	if !m.pendingCollapsed {
		parts = append(parts, m.lists[0].View())
	}

	parts = append(parts, reviewedHeader)
	if !m.reviewedCollapsed {
		parts = append(parts, m.lists[1].View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderMyPRsSections renders the Active + Drafts stacked sections for tab 1.
func renderMyPRsSections(m Model) string {
	activeHeader := renderSectionHeader("Active", len(m.lists[2].Items()), m.myPRsSection == 0, m.activeCollapsed)
	draftsHeader := renderSectionHeader("Drafts", len(m.lists[3].Items()), m.myPRsSection == 1, m.draftsCollapsed)

	var parts []string

	parts = append(parts, activeHeader)
	if !m.activeCollapsed {
		parts = append(parts, m.lists[2].View())
	}

	parts = append(parts, draftsHeader)
	if !m.draftsCollapsed {
		parts = append(parts, m.lists[3].View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderSectionHeader renders a collapsible section header like "v Pending (3)".
func renderSectionHeader(title string, count int, focused, collapsed bool) string {
	indicator := "v"
	if collapsed {
		indicator = ">"
	}

	label := fmt.Sprintf("%s %s (%d)", indicator, title, count)

	if focused {
		return sectionFocusedStyle.Render(label)
	}
	return sectionDimStyle.Render(label)
}

// renderHeader renders the title and tab bar.
func renderHeader(m Model) string {
	title := titleStyle.Render("PR Monitor")

	var tabs []string
	for i, tab := range m.tabs {
		var label string
		switch i {
		case 0:
			// Tab 0: show pending:reviewed counts.
			label = fmt.Sprintf("%s (%d:%d)", tab, len(m.lists[0].Items()), len(m.lists[1].Items()))
		case 1:
			// Tab 1: active:draft counts from lists[2] and lists[3].
			label = fmt.Sprintf("%s (%d:%d)", tab, len(m.lists[2].Items()), len(m.lists[3].Items()))
		case 2:
			// Tab 2: Stats with optional progress percentage.
			if m.statsProgress.Total > 0 && m.statsProgress.Done < m.statsProgress.Total {
				pct := m.statsProgress.Done * 100 / m.statsProgress.Total
				label = fmt.Sprintf("%s (%d%%)", tab, pct)
			} else {
				label = tab
			}
		}
		if i == m.activeTab {
			tabs = append(tabs, activeTabStyle.Render(label))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(label))
		}
	}

	tabBar := strings.Join(tabs, " ")
	return headerStyle.Render(title + "  " + tabBar)
}

// renderFooter renders keybinding hints with optional scroll percentage.
func renderFooter(m Model) string {
	if m.detailFocused {
		expandLabel := "e:expand"
		if m.commentsExpanded {
			expandLabel = "e:collapse"
		}
		legend := "j/k:scroll | r:refresh | " + expandLabel + " | g/G:top/bottom | h:back to list | ?:help | q:quit"
		if m.detailReady && m.activeDetail != nil {
			pct := m.detailViewport.ScrollPercent()
			legend += fmt.Sprintf(" %d%%", int(pct*100))
		}
		return footerStyle.Render(legend)
	}
	var legend string
	switch {
	case m.activeTab == 2:
		if m.statsViewMode == statsViewUser {
			legend = "tab:switch | j/k:cycle users | u:back to team | w:weekly | m:monthly | ?:help | q:quit"
		} else {
			legend = "tab:switch | u:user/team | w:weekly | m:monthly | j/k:scroll | ?:help | q:quit"
		}
	case m.activeTab == 1:
		legend = "tab:switch | r:address comments | d:dismiss | o:open | y:copy url | R:refresh | ?:help | q:quit"
		legend += " | s:section | x:fold"
	default:
		legend = "tab:switch | r:review | d:dismiss | o:open | y:copy url | R:refresh | ?:help | q:quit"
		legend += " | s:section | x:fold"
	}
	if m.activeTab != 2 && m.detailFetcher != nil && m.width >= 80 {
		legend += " | l:detail | ctrl+d/u:scroll"
		if m.detailReady && m.activeDetail != nil {
			pct := m.detailViewport.ScrollPercent()
			legend += fmt.Sprintf(" %d%%", int(pct*100))
		}
	}
	return footerStyle.Render(legend)
}

// renderHelpOverlay renders a full-screen help overlay with all keybindings.
func renderHelpOverlay(m Model) string {
	bindings := []struct{ key, desc string }{
		{"tab / shift+tab", "Switch tabs"},
		{"s", "Switch section (Pending/Reviewed, Active/Drafts)"},
		{"x", "Collapse/expand section"},
		{"r / enter", "Launch review (To Review) / Address comments (My PRs)"},
		{"d", "Dismiss PR"},
		{"o", "Open PR in browser"},
		{"y", "Copy PR URL to clipboard"},
		{"R", "Force refresh"},
		{"/", "Filter list"},
		{"l / h", "Focus detail panel / back to list"},
		{"j / k", "Scroll detail (when focused)"},
		{"r", "Refresh detail (when focused)"},
		{"e", "Expand / collapse comments (when focused)"},
		{"g / G", "Detail top / bottom (when focused)"},
		{"ctrl+d / ctrl+u", "Half-page scroll detail"},
		{"", "--- Stats Tab ---"},
		{"u", "Toggle team / user view"},
		{"j / k", "Cycle users (user view) / scroll (team view)"},
		{"w", "Weekly sparkline granularity"},
		{"m", "Monthly sparkline granularity"},
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
