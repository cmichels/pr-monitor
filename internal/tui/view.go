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

// loadingStyle for the spinner loading indicator.
var loadingStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("245")).
	Italic(true).
	Padding(1, 2)

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

		// Show spinner for initial loads (empty data + loading).
		initialLoading := false
		switch m.activeTab {
		case 0:
			initialLoading = m.prsLoading && len(m.lists[0].Items()) == 0 && len(m.lists[1].Items()) == 0
		case 1:
			initialLoading = m.prsLoading && len(m.lists[2].Items()) == 0 && len(m.lists[3].Items()) == 0
		case 3:
			initialLoading = m.jiraLoading && len(m.lists[4].Items()) == 0
		case 4:
			initialLoading = m.epicLoading && len(m.epicLists) == 0
		case 5:
			initialLoading = m.sprintLoading && len(m.sprintList.Items()) == 0
		}

		if initialLoading {
			listView = loadingStyle.Render(m.spinner.View() + " Loading...")
		} else {
			switch m.activeTab {
			case 0:
				listView = renderStackedSections(m)
			case 1:
				listView = renderMyPRsSections(m)
			case 3:
				listView = renderJiraTab(m)
			case 4:
				listView = renderEpicsTab(m)
			case 5:
				listView = renderSprintTab(m)
			default:
				listView = m.lists[2].View()
			}
		}

		isJiraTab := m.activeTab == 3 || m.activeTab == 4 || m.activeTab == 5
		showDetail := m.width >= 80 && m.detailReady && ((isJiraTab && m.jiraDetailFetcher != nil) || (!isJiraTab && m.detailFetcher != nil))
		if showDetail {
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

// renderStackedSections renders the Pending + Reviewed + Dismissed stacked sections for tab 0.
func renderStackedSections(m Model) string {
	pendingHeader := renderSectionHeader("Pending", len(m.lists[0].Items()), m.reviewSection == 0, m.pendingCollapsed)
	reviewedHeader := renderSectionHeader("Reviewed", len(m.lists[1].Items()), m.reviewSection == 1, m.reviewedCollapsed)
	dismissedHeader := renderSectionHeader("Dismissed", len(m.lists[8].Items()), m.reviewSection == 2, m.dismissedReviewerCollapsed)

	var parts []string

	parts = append(parts, pendingHeader)
	if !m.pendingCollapsed {
		parts = append(parts, m.lists[0].View())
	}

	parts = append(parts, reviewedHeader)
	if !m.reviewedCollapsed {
		parts = append(parts, m.lists[1].View())
	}

	parts = append(parts, dismissedHeader)
	if !m.dismissedReviewerCollapsed {
		parts = append(parts, m.lists[8].View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderMyPRsSections renders the Active + Drafts + Dismissed stacked sections for tab 1.
func renderMyPRsSections(m Model) string {
	activeHeader := renderSectionHeader("Active", len(m.lists[2].Items()), m.myPRsSection == 0, m.activeCollapsed)
	draftsHeader := renderSectionHeader("Drafts", len(m.lists[3].Items()), m.myPRsSection == 1, m.draftsCollapsed)
	dismissedHeader := renderSectionHeader("Dismissed", len(m.lists[9].Items()), m.myPRsSection == 2, m.dismissedAuthoredCollapsed)

	var parts []string

	parts = append(parts, activeHeader)
	if !m.activeCollapsed {
		parts = append(parts, m.lists[2].View())
	}

	parts = append(parts, draftsHeader)
	if !m.draftsCollapsed {
		parts = append(parts, m.lists[3].View())
	}

	parts = append(parts, dismissedHeader)
	if !m.dismissedAuthoredCollapsed {
		parts = append(parts, m.lists[9].View())
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
			if m.prsLoading {
				label += " " + m.spinner.View()
			}
		case 1:
			// Tab 1: active:draft counts from lists[2] and lists[3].
			label = fmt.Sprintf("%s (%d:%d)", tab, len(m.lists[2].Items()), len(m.lists[3].Items()))
			if m.prsLoading {
				label += " " + m.spinner.View()
			}
		case 2:
			// Tab 2: Stats with optional progress percentage.
			if m.statsProgress.Total > 0 && m.statsProgress.Done < m.statsProgress.Total {
				pct := m.statsProgress.Done * 100 / m.statsProgress.Total
				label = fmt.Sprintf("%s (%d%%)", tab, pct)
			} else {
				label = tab
			}
		case 3:
			// Tab 3: Jira total count.
			total := len(m.lists[4].Items()) + len(m.lists[5].Items()) + len(m.lists[6].Items()) + len(m.lists[7].Items())
			label = fmt.Sprintf("%s (%d)", tab, total)
			if m.jiraLoading {
				label += " " + m.spinner.View()
			}
		case 4:
			// Tab 4: Epics total count across all epic lists.
			total := 0
			for _, el := range m.epicLists {
				total += len(el.Items())
			}
			label = fmt.Sprintf("%s (%d)", tab, total)
			if m.epicLoading {
				label += " " + m.spinner.View()
			}
		case 5:
			// Tab 5: Sprint issue count.
			total := m.sprintStats.Total
			label = fmt.Sprintf("%s (%d)", tab, total)
			if m.sprintLoading {
				label += " " + m.spinner.View()
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
	case m.activeTab == 3:
		legend = "tab:switch | r:worktree | c:claim | o:open | y:copy url | R:refresh | s:section | x:fold | ?:help | q:quit"
	case m.activeTab == 4:
		legend = "tab:switch | r:worktree | c:claim | o:open | y:copy url | s:section | x:fold | e:others | a:add | t:hide | D:remove | ?:help | q:quit"
	case m.activeTab == 5:
		legend = "tab:switch | r:worktree | c:claim | o:open | y:copy url | e:others | ?:help | q:quit"
	case m.activeTab == 1:
		if m.myPRsSection == 2 {
			legend = "tab:switch | u:restore | o:open | y:copy url | R:refresh | ?:help | q:quit"
		} else {
			legend = "tab:switch | r:address comments | d:dismiss | o:open | y:copy url | R:refresh | ?:help | q:quit"
		}
		legend += " | s:section | x:fold"
	default:
		if m.reviewSection == 2 {
			legend = "tab:switch | u:restore | o:open | y:copy url | R:refresh | ?:help | q:quit"
		} else {
			legend = "tab:switch | r:review | d:dismiss | o:open | y:copy url | R:refresh | ?:help | q:quit"
		}
		legend += " | s:section | x:fold"
	}
	if m.activeTab != 2 && m.width >= 80 {
		isJiraTab := m.activeTab == 3 || m.activeTab == 4 || m.activeTab == 5
		hasDetail := (isJiraTab && m.jiraDetailFetcher != nil) || (!isJiraTab && m.detailFetcher != nil)
		if hasDetail {
			legend += " | l:detail | ctrl+d/u:scroll"
			hasContent := (isJiraTab && m.jiraDetail != nil) || (!isJiraTab && m.activeDetail != nil)
			if m.detailReady && hasContent {
				pct := m.detailViewport.ScrollPercent()
				legend += fmt.Sprintf(" %d%%", int(pct*100))
			}
		}
	}
	return footerStyle.Render(legend)
}

// renderHelpOverlay renders a full-screen help overlay with all keybindings.
func renderHelpOverlay(m Model) string {
	bindings := []struct{ key, desc string }{
		{"tab / shift+tab", "Switch tabs"},
		{"s", "Cycle section (Pending/Reviewed/Dismissed, Active/Drafts/Dismissed)"},
		{"x", "Collapse/expand section"},
		{"r / enter", "Launch review (To Review) / Address comments (My PRs)"},
		{"d", "Dismiss PR"},
		{"u", "Restore dismissed PR (when in Dismissed section)"},
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
		{"", "--- Jira Tab ---"},
		{"r / enter", "Launch worktree for issue"},
		{"c", "Claim issue (assign to me + In Progress)"},
		{"o", "Open issue in browser"},
		{"y", "Copy issue URL"},
		{"s", "Cycle sections (In Progress/Submissions/Knowledge/My Tasks)"},
		{"", "--- Epics Tab ---"},
		{"r / enter", "Launch worktree for issue"},
		{"c", "Claim issue"},
		{"o", "Open issue in browser"},
		{"y", "Copy issue URL"},
		{"s", "Cycle epic sections"},
		{"x", "Collapse/expand epic section"},
		{"e", "Toggle showing items assigned to others"},
		{"a", "Add tracked epic"},
		{"t", "Hide epic (toggle active)"},
		{"D", "Remove tracked epic"},
		{"", "--- Sprint Tab ---"},
		{"r / enter", "Launch worktree for issue"},
		{"c", "Claim issue (assign to me + In Progress)"},
		{"o", "Open issue in browser"},
		{"y", "Copy issue URL"},
		{"e", "Toggle showing items assigned to others"},
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
