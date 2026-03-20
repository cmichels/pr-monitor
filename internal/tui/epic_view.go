package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderEpicsTab renders the dynamic N stacked sections for the Epics tab.
func renderEpicsTab(m Model) string {
	// Show the add epic input prompt if active.
	if m.epicAddActive {
		promptStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)
		inputLine := promptStyle.Render("Add epic key:") + " " + m.epicAddInput.View()
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Faint(true).
			Padding(0, 1)
		hint := hintStyle.Render("Enter to confirm, Esc to cancel")
		return lipgloss.JoinVertical(lipgloss.Left, inputLine, hint)
	}

	if len(m.epicSections) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true).
			Padding(1, 2)
		msg := "No tracked epics."
		if m.epicManager != nil {
			msg += " Press 'a' to add one."
		} else {
			msg += " Add epics in config.yaml under jira.epics"
		}
		return emptyStyle.Render(msg)
	}

	othersHintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Faint(true).
		Padding(0, 3)

	var parts []string
	for i, sec := range m.epicSections {
		var stats EpicStats
		if i < len(m.epicStats) {
			stats = m.epicStats[i]
		}
		focused := m.epicSection == i
		collapsed := m.epicCollapsed[i]
		header := renderEpicSectionHeader(sec, stats, focused, collapsed)
		parts = append(parts, header)
		if !collapsed && i < len(m.epicLists) {
			parts = append(parts, m.epicLists[i].View())
			// Show "others" hint when they're hidden.
			if i < len(m.epicPartitions) && i < len(m.epicShowOthers) {
				othersCount := len(m.epicPartitions[i].others)
				if othersCount > 0 && !m.epicShowOthers[i] {
					hint := fmt.Sprintf("  %d assigned to others [e to show]", othersCount)
					parts = append(parts, othersHintStyle.Render(hint))
				} else if othersCount > 0 && m.epicShowOthers[i] {
					hint := fmt.Sprintf("  showing all · %d others [e to hide]", othersCount)
					parts = append(parts, othersHintStyle.Render(hint))
				}
			}
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderEpicSectionHeader renders an enhanced section header with progress bar and stats.
// Format: v OP-3309 UX/UI Design  ████████░░░░ 65% · 13/20 done · 3 mine · 1 blocked
// The stats line is always visible, even when collapsed (dashboard-at-a-glance).
func renderEpicSectionHeader(sec EpicSection, stats EpicStats, focused, collapsed bool) string {
	indicator := "v"
	if collapsed {
		indicator = ">"
	}

	label := fmt.Sprintf("%s %s %s", indicator, sec.EpicKey, sec.Name)

	if stats.Total == 0 {
		label += " (0)"
		if focused {
			return sectionFocusedStyle.Render(label)
		}
		return sectionDimStyle.Render(label)
	}

	// Progress bar: 12 chars wide.
	bar := renderProgressBar(stats.Done, stats.Total, 12)
	pct := stats.Done * 100 / stats.Total

	// Build stats suffix.
	suffix := fmt.Sprintf(" %s %d%% · %d/%d done", bar, pct, stats.Done, stats.Total)
	if stats.MyCount > 0 {
		suffix += fmt.Sprintf(" · %d mine", stats.MyCount)
	}
	if stats.Blocked > 0 {
		suffix += fmt.Sprintf(" · %d blocked", stats.Blocked)
	}
	if stats.Unassigned > 0 {
		suffix += fmt.Sprintf(" · %d unassigned", stats.Unassigned)
	}

	label += suffix

	if focused {
		return sectionFocusedStyle.Render(label)
	}
	return sectionDimStyle.Render(label)
}

// renderProgressBar creates a text-based progress bar of the given width.
// Uses block characters: filled (█) and empty (░).
func renderProgressBar(done, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}

	filledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // green
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))  // dim gray

	return filledStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", width-filled))
}

// resizeEpicSections recalculates heights for the dynamic N epic section lists.
func (m *Model) resizeEpicSections() {
	if len(m.epicLists) == 0 {
		return
	}

	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.jiraDetailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	n := len(m.epicSections)
	// Reserve 1 line per section header + 1 line per "others" hint on expanded sections.
	overhead := n
	for i := 0; i < n; i++ {
		if !m.epicCollapsed[i] && i < len(m.epicPartitions) && len(m.epicPartitions[i].others) > 0 {
			overhead++
		}
	}
	available := listHeight - overhead
	if available < 0 {
		available = 0
	}

	expanded := 0
	for i := 0; i < n; i++ {
		if !m.epicCollapsed[i] {
			expanded++
		}
	}

	if expanded == 0 {
		for i := range m.epicLists {
			m.epicLists[i].SetSize(listWidth, 0)
		}
		return
	}

	each := available / expanded
	remainder := available - each*expanded
	assigned := 0
	for i := 0; i < n; i++ {
		if m.epicCollapsed[i] {
			m.epicLists[i].SetSize(listWidth, 0)
		} else {
			h := each
			if assigned == 0 {
				h += remainder
			}
			m.epicLists[i].SetSize(listWidth, h)
			assigned++
		}
	}
}

// epicItemDelegate is a custom list delegate that renders group headers
// as dim separator lines (1 line tall) and regular JiraItems using
// the standard DefaultDelegate (2 lines: title + description).
type epicItemDelegate struct {
	inner list.DefaultDelegate
}

func newEpicDelegate() epicItemDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	return epicItemDelegate{inner: d}
}

func (d epicItemDelegate) Height() int                             { return d.inner.Height() }
func (d epicItemDelegate) Spacing() int                            { return d.inner.Spacing() }
func (d epicItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return d.inner.Update(msg, m) }

func (d epicItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if _, ok := item.(epicGroupHeader); ok {
		// Render group header as a single styled line with a blank second line
		// to match the 2-line height of regular items.
		width := m.Width()
		selected := index == m.Index()

		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Faint(true).
			Width(width)

		if selected {
			headerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true).
				Width(width)
		}

		title := item.(epicGroupHeader).label
		fmt.Fprint(w, headerStyle.Render("  "+title))
		fmt.Fprint(w, "\n")
		return
	}

	// Regular JiraItem: delegate to the inner default delegate.
	d.inner.Render(w, m, index, item)
}
