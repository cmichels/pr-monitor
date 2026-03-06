package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Stats view styles.
var (
	statsHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205")).
				Padding(0, 1)

	statsLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Width(20)

	statsValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Bold(true)

	statsSparkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	statsRankStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("110"))

	statsProgressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205"))

	statsProgressDimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	statsHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)
)

// renderStatsTab renders the full stats tab content for the viewport.
func renderStatsTab(m Model) string {
	if !m.statsReady {
		return renderStatsProgress(m)
	}
	if m.statsData == nil {
		return statsProgressDimStyle.Render("  Loading stats...")
	}

	switch m.statsViewMode {
	case statsViewUser:
		return renderUserView(m)
	default:
		return renderTeamView(m)
	}
}

// renderStatsProgress renders the backfill progress bar.
func renderStatsProgress(m Model) string {
	p := m.statsProgress
	if p.Total == 0 {
		return statsProgressDimStyle.Render("  Waiting for stats backfill...")
	}

	barWidth := 30
	filled := barWidth * p.Done / p.Total
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("=", filled)
	if filled < barWidth {
		bar += ">"
		bar += strings.Repeat(" ", barWidth-filled-1)
	}

	return fmt.Sprintf("  %s %d/%d %s",
		statsProgressBarStyle.Render("["+bar+"]"),
		p.Done, p.Total,
		statsProgressDimStyle.Render(p.Current),
	)
}

// renderTeamView renders the team-level stats overview with GitHub on the left
// and Jira on the right in a two-column layout.
func renderTeamView(m Model) string {
	leftCol := renderGitHubColumn(m)
	rightCol := renderJiraColumn(m)

	if rightCol == "" {
		return leftCol
	}

	separator := statsColumnSeparator(leftCol)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, separator, rightCol)
}

// statsColumnSeparator builds a thin vertical divider sized to match the
// content height — same visual pattern as separatorView in view.go.
func statsColumnSeparator(reference string) string {
	height := strings.Count(reference, "\n") + 1
	if height < 1 {
		height = 1
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	lines := make([]string, height)
	for i := range lines {
		lines[i] = style.Render(" \u2502 ")
	}
	return strings.Join(lines, "\n")
}

// renderGitHubColumn renders the GitHub stats: header, sparklines, and leaderboards.
func renderGitHubColumn(m Model) string {
	var b strings.Builder

	gran := "weekly"
	if m.statsGranularity == statsGranMonthly {
		gran = "monthly"
	}

	yearLabel := "2026"
	if len(m.statsData.Days) > 0 && len(m.statsData.Days[0].Date) >= 4 {
		yearLabel = m.statsData.Days[0].Date[:4]
	}
	b.WriteString("  ")
	b.WriteString(statsHeaderStyle.Render(yearLabel + " Team Stats"))
	b.WriteString("  ")
	b.WriteString(statsHintStyle.Render(fmt.Sprintf("[%s]  u:user view  w:weekly  m:monthly", gran)))
	b.WriteString("\n\n")

	days := m.statsData.Days

	// Metric sparklines.
	type metric struct {
		label string
		fn    func(StatsDay) int
	}
	metrics := []metric{
		{"PRs Created", func(d StatsDay) int { return d.PRsCreated }},
		{"PRs Merged", func(d StatsDay) int { return d.PRsMerged }},
		{"PRs Reviewed", func(d StatsDay) int { return d.PRsReviewed }},
		{"Comments Given", func(d StatsDay) int { return d.CommentsGiven }},
		{"Approvals Given", func(d StatsDay) int { return d.ApprovalsGiven }},
		{"Changes Requested", func(d StatsDay) int { return d.ChangesRequested }},
		{"Lines Added", func(d StatsDay) int { return d.LinesAdded }},
		{"Lines Removed", func(d StatsDay) int { return d.LinesRemoved }},
	}

	for _, met := range metrics {
		var bins []int
		if m.statsGranularity == statsGranMonthly {
			bins = aggregateByMonth(days, met.fn)
		} else {
			bins = aggregateByWeek(days, met.fn)
		}
		total := sumMetric(days, met.fn)
		spark := renderSparkline(bins)

		b.WriteString("  ")
		b.WriteString(statsLabelStyle.Render(met.label))
		b.WriteString(" ")
		b.WriteString(statsSparkStyle.Render(spark))
		b.WriteString("  ")
		b.WriteString(statsValueStyle.Render(formatCount(total)))
		b.WriteString("\n")
	}

	// Top Reviewers.
	b.WriteString("\n  ")
	b.WriteString(statsHeaderStyle.Render("Top Reviewers"))
	b.WriteString("\n")
	for i, u := range m.statsData.TopReviewers {
		b.WriteString(fmt.Sprintf("  %s %s  %s reviewed  %s approved  %s comments\n",
			statsProgressDimStyle.Render(fmt.Sprintf("%2d.", i+1)),
			statsRankStyle.Render(fmt.Sprintf("%-20s", "@"+u.Login)),
			statsValueStyle.Render(fmt.Sprintf("%4d", u.PRsReviewed)),
			statsValueStyle.Render(fmt.Sprintf("%4d", u.ApprovalsGiven)),
			statsValueStyle.Render(fmt.Sprintf("%4d", u.CommentsGiven)),
		))
	}

	// Top Authors.
	b.WriteString("\n  ")
	b.WriteString(statsHeaderStyle.Render("Top Authors"))
	b.WriteString("\n")
	for i, u := range m.statsData.TopAuthors {
		b.WriteString(fmt.Sprintf("  %s %s  %s merged  %s created  %s +%s -%s lines\n",
			statsProgressDimStyle.Render(fmt.Sprintf("%2d.", i+1)),
			statsRankStyle.Render(fmt.Sprintf("%-20s", "@"+u.Login)),
			statsValueStyle.Render(fmt.Sprintf("%4d", u.PRsMerged)),
			statsValueStyle.Render(fmt.Sprintf("%4d", u.PRsCreated)),
			statsProgressDimStyle.Render(""),
			statsValueStyle.Render(formatCount(u.LinesAdded)),
			statsValueStyle.Render(formatCount(u.LinesRemoved)),
		))
	}

	return b.String()
}

// renderJiraColumn renders the Jira stats column: YTD and monthly leaderboards.
// Returns "" if no Jira data is available, signaling the caller to use full-width GitHub.
func renderJiraColumn(m Model) string {
	hasYTD := len(m.statsData.JiraYTD) > 0
	hasMonth := len(m.statsData.JiraMonth) > 0
	if !hasYTD && !hasMonth {
		return ""
	}

	var b strings.Builder

	b.WriteString("  ")
	b.WriteString(statsHeaderStyle.Render("Jira Stats"))
	b.WriteString("\n\n")

	if hasYTD {
		b.WriteString("  ")
		b.WriteString(statsHeaderStyle.Render(fmt.Sprintf("YTD (%d)", time.Now().Year())))
		b.WriteString("\n")
		renderJiraLeaderboard(&b, m.statsData.JiraYTD)
	}

	if hasMonth {
		b.WriteString("\n  ")
		b.WriteString(statsHeaderStyle.Render(fmt.Sprintf("%s %d", time.Now().Month().String(), time.Now().Year())))
		b.WriteString("\n")
		renderJiraLeaderboard(&b, m.statsData.JiraMonth)
	}

	return b.String()
}

// renderUserView renders stats for a single user.
func renderUserView(m Model) string {
	if len(m.statsUsers) == 0 {
		return statsProgressDimStyle.Render("  No users loaded")
	}

	login := m.statsUsers[m.statsUserIdx]

	var b strings.Builder

	gran := "weekly"
	if m.statsGranularity == statsGranMonthly {
		gran = "monthly"
	}

	b.WriteString("  ")
	b.WriteString(statsHeaderStyle.Render("@" + login))
	b.WriteString("  ")
	b.WriteString(statsHintStyle.Render(fmt.Sprintf("[%s]  j/k:prev/next  u:back to team  w:weekly  m:monthly", gran)))
	b.WriteString(fmt.Sprintf("  (%d/%d)", m.statsUserIdx+1, len(m.statsUsers)))
	b.WriteString("\n\n")

	days := filterByLogin(m.statsData.Days, login)

	type metric struct {
		label string
		fn    func(StatsDay) int
	}
	metrics := []metric{
		{"PRs Created", func(d StatsDay) int { return d.PRsCreated }},
		{"PRs Merged", func(d StatsDay) int { return d.PRsMerged }},
		{"PRs Reviewed", func(d StatsDay) int { return d.PRsReviewed }},
		{"Comments Given", func(d StatsDay) int { return d.CommentsGiven }},
		{"Approvals Given", func(d StatsDay) int { return d.ApprovalsGiven }},
		{"Changes Requested", func(d StatsDay) int { return d.ChangesRequested }},
		{"Lines Added", func(d StatsDay) int { return d.LinesAdded }},
		{"Lines Removed", func(d StatsDay) int { return d.LinesRemoved }},
	}

	for _, met := range metrics {
		var bins []int
		if m.statsGranularity == statsGranMonthly {
			bins = aggregateByMonth(days, met.fn)
		} else {
			bins = aggregateByWeek(days, met.fn)
		}
		total := sumMetric(days, met.fn)
		spark := renderSparkline(bins)

		b.WriteString("  ")
		b.WriteString(statsLabelStyle.Render(met.label))
		b.WriteString(" ")
		b.WriteString(statsSparkStyle.Render(spark))
		b.WriteString("  ")
		b.WriteString(statsValueStyle.Render(formatCount(total)))
		b.WriteString("\n")
	}

	return b.String()
}

// sparkBlocks are the Unicode block characters for sparklines (8 levels).
var sparkBlocks = []rune{' ', '\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}

// renderSparkline renders a slice of integers as a Unicode sparkline.
func renderSparkline(bins []int) string {
	if len(bins) == 0 {
		return ""
	}

	maxVal := 0
	for _, v := range bins {
		if v > maxVal {
			maxVal = v
		}
	}

	var sb strings.Builder
	for _, v := range bins {
		if maxVal == 0 {
			sb.WriteRune(sparkBlocks[0])
		} else {
			idx := v * 8 / maxVal
			if idx > 8 {
				idx = 8
			}
			sb.WriteRune(sparkBlocks[idx])
		}
	}
	return sb.String()
}

// aggregateByWeek groups daily stats into ISO week buckets.
func aggregateByWeek(days []StatsDay, metric func(StatsDay) int) []int {
	if len(days) == 0 {
		return nil
	}

	// Determine the year from the first entry.
	year := 2026
	if len(days) > 0 {
		if t, err := time.Parse("2006-01-02", days[0].Date); err == nil {
			year = t.Year()
		}
	}

	// Pre-allocate 53 weeks (max ISO weeks).
	bins := make([]int, 53)
	maxWeek := 0

	for _, d := range days {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		_, week := t.ISOWeek()
		weekIdx := week - 1
		if weekIdx < 0 {
			weekIdx = 0
		}
		if weekIdx >= len(bins) {
			weekIdx = len(bins) - 1
		}
		bins[weekIdx] += metric(d)
		if weekIdx > maxWeek {
			maxWeek = weekIdx
		}
	}

	// Trim to current week of the year.
	now := time.Date(year, time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.UTC)
	_, currentWeek := now.ISOWeek()
	endWeek := currentWeek
	if maxWeek > endWeek {
		endWeek = maxWeek
	}
	if endWeek >= len(bins) {
		endWeek = len(bins) - 1
	}

	return bins[:endWeek+1]
}

// aggregateByMonth groups daily stats into month buckets (0=Jan, 11=Dec).
func aggregateByMonth(days []StatsDay, metric func(StatsDay) int) []int {
	bins := make([]int, 12)
	maxMonth := 0

	for _, d := range days {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		monthIdx := int(t.Month()) - 1
		bins[monthIdx] += metric(d)
		if monthIdx > maxMonth {
			maxMonth = monthIdx
		}
	}

	// Trim to current month.
	currentMonth := int(time.Now().Month()) - 1
	endMonth := currentMonth
	if maxMonth > endMonth {
		endMonth = maxMonth
	}

	return bins[:endMonth+1]
}

// filterByLogin returns only the StatsDay entries for the given login.
func filterByLogin(days []StatsDay, login string) []StatsDay {
	var result []StatsDay
	for _, d := range days {
		if d.Login == login {
			result = append(result, d)
		}
	}
	return result
}

// sumMetric sums a metric function across all days.
func sumMetric(days []StatsDay, metric func(StatsDay) int) int {
	total := 0
	for _, d := range days {
		total += metric(d)
	}
	return total
}

// renderJiraLeaderboard renders a ranked list of Jira user stats.
func renderJiraLeaderboard(b *strings.Builder, stats []JiraUserStat) {
	for i, s := range stats {
		b.WriteString(fmt.Sprintf("  %s %s  %s created  %s finished\n",
			statsProgressDimStyle.Render(fmt.Sprintf("%2d.", i+1)),
			statsRankStyle.Render(fmt.Sprintf("%-20s", s.DisplayName)),
			statsValueStyle.Render(fmt.Sprintf("%4d", s.CreatedCount)),
			statsValueStyle.Render(fmt.Sprintf("%4d", s.FinishedCount)),
		))
	}
}

// formatCount formats a number with comma separators or K/M suffix for large values.
func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
