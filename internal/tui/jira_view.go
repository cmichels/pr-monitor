package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Jira detail panel styles.
var (
	jiraKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	jiraLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Width(12)

	jiraValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	jiraDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("236"))
)

// renderJiraTab renders the 4 stacked sections for the Jira tab.
func renderJiraTab(m Model) string {
	names := m.jiraSectionNames()
	var parts []string

	for i := 0; i < 4; i++ {
		listIdx := 4 + i
		header := renderSectionHeader(names[i], len(m.lists[listIdx].Items()), m.jiraSection == i, m.jiraCollapsed[i])
		parts = append(parts, header)
		if !m.jiraCollapsed[i] {
			parts = append(parts, m.lists[listIdx].View())
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderJiraDetailPanel renders the detail panel content for a Jira issue.
func renderJiraDetailPanel(m Model) string {
	vpWidth := m.detailViewport.Width
	if vpWidth < 10 {
		vpWidth = 10
	}

	if m.jiraDetailLoading {
		return detailLoadingStyle.Render("  Loading...")
	}
	if m.jiraDetailErr != nil {
		return detailErrorStyle.Render(fmt.Sprintf("  Error: %v", m.jiraDetailErr))
	}
	if m.jiraDetail == nil {
		return detailLoadingStyle.Render("  Select an issue to view details")
	}

	return renderJiraDetailContent(m.jiraDetail, vpWidth)
}

// renderJiraDetailContent renders the JiraDetail into styled sections.
func renderJiraDetailContent(detail *JiraDetail, contentWidth int) string {
	inner := contentWidth - 2
	if inner < 10 {
		inner = 10
	}

	var b strings.Builder

	// Header: key + type
	b.WriteString("  ")
	b.WriteString(jiraKeyStyle.Render(detail.Key))
	b.WriteString("  ")
	b.WriteString(jiraValueStyle.Render(detail.IssueType))
	b.WriteString("\n")

	// Divider
	b.WriteString("  ")
	b.WriteString(jiraDividerStyle.Render(strings.Repeat("-", inner)))
	b.WriteString("\n")

	// Metadata fields
	fields := []struct{ label, value string }{
		{"Status", detail.Status},
		{"Priority", detail.Priority},
		{"Assignee", nvl(detail.Assignee, "(unassigned)")},
		{"Reporter", nvl(detail.Reporter, "(unknown)")},
	}
	if len(detail.Labels) > 0 {
		fields = append(fields, struct{ label, value string }{"Labels", strings.Join(detail.Labels, ", ")})
	}

	for _, f := range fields {
		b.WriteString("  ")
		b.WriteString(jiraLabelStyle.Render(f.label))
		b.WriteString(jiraValueStyle.Render(f.value))
		b.WriteString("\n")
	}

	// Description
	b.WriteString("\n  ")
	b.WriteString(detailSectionStyle.Render("Description"))
	b.WriteString("\n")
	desc := detail.Description
	if desc == "" {
		desc = "(no description)"
	}
	if len(desc) > 3000 {
		desc = desc[:3000] + "..."
	}
	wrapped := lipgloss.NewStyle().Width(inner).Render(desc)
	for _, line := range strings.Split(wrapped, "\n") {
		b.WriteString("  ")
		b.WriteString(detailBodyStyle.Render(line))
		b.WriteString("\n")
	}

	// Comments
	b.WriteString("\n  ")
	b.WriteString(detailSectionStyle.Render(fmt.Sprintf("Comments (%d)", len(detail.Comments))))
	b.WriteString("\n")
	if len(detail.Comments) == 0 {
		b.WriteString("  ")
		b.WriteString(checkDimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		for i, c := range detail.Comments {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  ")
			b.WriteString(commentAuthorStyle.Render(c.Author))
			if c.CreatedAt != "" {
				b.WriteString(" ")
				b.WriteString(commentTimeStyle.Render(c.CreatedAt))
			}
			b.WriteString("\n")
			if c.Body != "" {
				bodyWrapped := lipgloss.NewStyle().Width(inner - 2).Render(c.Body)
				for _, line := range strings.Split(bodyWrapped, "\n") {
					b.WriteString("    ")
					b.WriteString(commentBodyStyle.Render(line))
					b.WriteString("\n")
				}
			}
		}
	}

	return clampWidth(b.String(), contentWidth)
}

// resizeJiraSections recalculates heights for the 4 Jira section lists.
func (m *Model) resizeJiraSections() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.detailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	// 4 section headers = 4 lines overhead.
	available := listHeight - 4
	if available < 0 {
		available = 0
	}

	expanded := 0
	for i := 0; i < 4; i++ {
		if !m.jiraCollapsed[i] {
			expanded++
		}
	}

	var heights [4]int
	if expanded > 0 {
		each := available / expanded
		remainder := available - each*expanded
		assigned := 0
		for i := 0; i < 4; i++ {
			if m.jiraCollapsed[i] {
				heights[i] = 0
			} else {
				heights[i] = each
				if assigned == 0 {
					heights[i] += remainder
				}
				assigned++
			}
		}
	}

	for i := 0; i < 4; i++ {
		m.lists[4+i].SetSize(listWidth, heights[i])
	}
}

// jiraSectionNames returns the display names for the 4 Jira sections.
func (m Model) jiraSectionNames() [4]string {
	return [4]string{"In Progress", "Submissions", "Knowledge", "My Tasks"}
}

// nvl returns val if non-empty, otherwise fallback.
func nvl(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
