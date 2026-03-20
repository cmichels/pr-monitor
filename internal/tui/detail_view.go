package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Detail panel styles.
var (
	detailSectionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	detailBodyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	fileAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	fileModStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	fileDeletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f44336"))
	fileDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	checkPassStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
	checkFailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f44336"))
	checkPendStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
	checkDimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	commentAuthorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("110"))

	commentTimeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	commentBodyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	reviewApprovedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4caf50")).
				Bold(true)

	reviewChangesStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f44336")).
				Bold(true)

	reviewCommentedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f9a825"))

	detailLoadingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Italic(true)

	detailErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
)

// updateDetailViewport renders the current detail state into the viewport.
func (m *Model) updateDetailViewport() {
	if !m.detailReady {
		return
	}
	var content string
	if m.activeTab == 3 || m.activeTab == 4 || m.activeTab == 5 {
		content = renderJiraDetailPanel(*m)
	} else {
		content = renderDetailPanel(*m)
	}
	m.detailViewport.SetContent(content)
	m.detailViewport.GotoTop()
}

// renderDetailPanel renders the detail panel content based on current state.
func renderDetailPanel(m Model) string {
	vpWidth := m.detailViewport.Width
	if vpWidth < 10 {
		vpWidth = 10
	}

	if m.detailLoading {
		return detailLoadingStyle.Render("  " + m.spinner.View() + " Loading...")
	}
	if m.detailErr != nil {
		return detailErrorStyle.Render(fmt.Sprintf("  Error: %v", m.detailErr))
	}
	if m.activeDetail == nil {
		return detailLoadingStyle.Render("  Select a PR to view details")
	}

	return renderDetailContent(m.activeDetail, vpWidth, m.commentsExpanded, m.activeTab)
}

// renderDetailContent renders the PRDetail into styled sections.
// contentWidth is the full viewport width — all lines must fit within it.
// activeTab controls which sections are shown (e.g. Approvals only on My PRs tab).
func renderDetailContent(detail *PRDetail, contentWidth int, commentsExpanded bool, activeTab int) string {
	// Inner width leaves 2 chars for left padding.
	inner := contentWidth - 2
	if inner < 10 {
		inner = 10
	}

	var b strings.Builder

	// -- Approvals (My PRs tab only) ------------------------------------------
	if activeTab == 1 && len(detail.Reviews) > 0 {
		b.WriteString("  ")
		b.WriteString(detailSectionStyle.Render(fmt.Sprintf("Approvals (%d)", len(detail.Reviews))))
		b.WriteString("\n")
		for _, r := range detail.Reviews {
			icon, style, label := approvalIcon(r.State)
			b.WriteString("  ")
			b.WriteString(style.Render(icon))
			b.WriteString(" ")
			b.WriteString(commentAuthorStyle.Render("@" + r.Author))
			b.WriteString("  ")
			b.WriteString(style.Render(label))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// -- Description ----------------------------------------------------------
	b.WriteString("  ")
	b.WriteString(detailSectionStyle.Render("Description"))
	b.WriteString("\n")
	body := detail.Body
	if body == "" {
		body = "(no description)"
	}
	if len(body) > 2000 {
		body = body[:2000] + "..."
	}
	// Use lipgloss Width for proper word-wrapping at display-width boundaries.
	wrapped := lipgloss.NewStyle().Width(inner).Render(body)
	for _, line := range strings.Split(wrapped, "\n") {
		b.WriteString("  ")
		b.WriteString(detailBodyStyle.Render(line))
		b.WriteString("\n")
	}

	// -- Comments ---------------------------------------------------------------
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
			// Author + review badge + time ago
			b.WriteString("  ")
			b.WriteString(commentAuthorStyle.Render("@" + c.Author))
			if c.ReviewState != "" {
				b.WriteString(" ")
				b.WriteString(reviewBadge(c.ReviewState))
			}
			b.WriteString(" ")
			b.WriteString(commentTimeStyle.Render(timeAgo(c.CreatedAt)))
			b.WriteString("\n")

			// Comment body, word-wrapped. Collapsed by default (3 lines max).
			body := c.Body
			if body != "" {
				wrapped := lipgloss.NewStyle().Width(inner - 2).Render(body)
				lines := strings.Split(wrapped, "\n")
				truncated := false
				if !commentsExpanded && len(lines) > 3 {
					lines = lines[:3]
					truncated = true
				}
				for _, line := range lines {
					b.WriteString("    ")
					b.WriteString(commentBodyStyle.Render(line))
					b.WriteString("\n")
				}
				if truncated {
					b.WriteString("    ")
					b.WriteString(checkDimStyle.Render("... [e to expand]"))
					b.WriteString("\n")
				}
			}
		}
	}

	// -- CI Checks ------------------------------------------------------------
	b.WriteString("\n  ")
	b.WriteString(detailSectionStyle.Render(fmt.Sprintf("CI Checks (%d)", len(detail.Checks))))
	b.WriteString("\n")
	if len(detail.Checks) == 0 {
		b.WriteString("  ")
		b.WriteString(checkDimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, c := range detail.Checks {
			icon, style := checkIcon(c)
			// Budget: 2(indent) + icon + 1(space) + name <= contentWidth
			nameMax := inner - lipgloss.Width(icon) - 1
			if nameMax < 10 {
				nameMax = 10
			}
			b.WriteString("  ")
			b.WriteString(style.Render(icon))
			b.WriteString(" ")
			b.WriteString(runesTruncate(c.Name, nameMax))
			b.WriteString("\n")
		}
	}

	// -- Files Changed --------------------------------------------------------
	b.WriteString("\n  ")
	b.WriteString(detailSectionStyle.Render(fmt.Sprintf("Files Changed (%d)", len(detail.Files))))
	b.WriteString("\n")
	if len(detail.Files) == 0 {
		b.WriteString("  ")
		b.WriteString(fileDimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, f := range detail.Files {
			marker, style := fileMarker(f)
			stats := fmt.Sprintf(" +%d -%d", f.Additions, f.Deletions)
			// Budget: 2(indent) + 1(marker) + 1(space) + path + stats <= contentWidth
			pathMax := inner - 2 - lipgloss.Width(stats)
			if pathMax < 10 {
				pathMax = 10
			}
			path := runesTruncate(f.Path, pathMax)
			b.WriteString("  ")
			b.WriteString(style.Render(marker))
			b.WriteString(" ")
			b.WriteString(path)
			b.WriteString(fileDimStyle.Render(stats))
			b.WriteString("\n")
		}
	}

	// Safety net: hard-clamp every line to contentWidth so the viewport
	// never produces lines wider than its column, which would cause terminal
	// line-wrapping and steal vertical space from the footer.
	return clampWidth(b.String(), contentWidth)
}

// approvalIcon returns the icon character, style, and label for a review state.
func approvalIcon(state string) (string, lipgloss.Style, string) {
	switch state {
	case "APPROVED":
		return "+", reviewApprovedStyle, "approved"
	case "CHANGES_REQUESTED":
		return "x", reviewChangesStyle, "changes requested"
	case "COMMENTED":
		return "~", reviewCommentedStyle, "commented"
	case "PENDING":
		return "o", checkPendStyle, "pending"
	default:
		return "?", checkDimStyle, state
	}
}

// fileMarker returns a marker character and style based on file change type.
func fileMarker(f FileChange) (string, lipgloss.Style) {
	if f.Deletions == 0 && f.Additions > 0 {
		return "A", fileAddedStyle
	}
	if f.Additions == 0 && f.Deletions > 0 {
		return "D", fileDeletedStyle
	}
	return "M", fileModStyle
}

// checkIcon returns an icon and style based on check conclusion/status.
func checkIcon(c Check) (string, lipgloss.Style) {
	switch {
	case c.Conclusion == "SUCCESS":
		return "ok", checkPassStyle
	case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED":
		return "FAIL", checkFailStyle
	case c.Status == "IN_PROGRESS" || c.Status == "QUEUED" || c.Conclusion == "PENDING":
		return "...", checkPendStyle
	default:
		return "?", checkDimStyle
	}
}

// runesTruncate truncates a string to maxWidth runes (display-width aware
// for ASCII; close enough for typical code/path content).
func runesTruncate(s string, maxWidth int) string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth < 4 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-3]) + "..."
}

// reviewBadge returns a styled label for a review state.
func reviewBadge(state string) string {
	switch state {
	case "APPROVED":
		return reviewApprovedStyle.Render("[approved]")
	case "CHANGES_REQUESTED":
		return reviewChangesStyle.Render("[changes requested]")
	case "COMMENTED":
		return reviewCommentedStyle.Render("[commented]")
	case "DISMISSED":
		return checkDimStyle.Render("[dismissed]")
	default:
		return checkDimStyle.Render("[review]")
	}
}

// timeAgo returns a human-friendly relative time string.
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// clampWidth ensures no line in s exceeds maxWidth display columns.
// Uses lipgloss.Width for accurate ANSI-aware measurement and
// lipgloss MaxWidth for truncation.
func clampWidth(s string, maxWidth int) string {
	lines := strings.Split(s, "\n")
	clamper := lipgloss.NewStyle().MaxWidth(maxWidth)
	for i, line := range lines {
		if lipgloss.Width(line) > maxWidth {
			lines[i] = clamper.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
