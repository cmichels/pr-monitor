package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// StatsDay is the TUI's view of a daily contributor stat row.
type StatsDay struct {
	Login            string
	Date             string
	PRsCreated       int
	PRsMerged        int
	PRsReviewed      int
	CommentsGiven    int
	LinesAdded       int
	LinesRemoved     int
	ApprovalsGiven   int
	ChangesRequested int
}

// UserTotals holds aggregated metrics for a single user.
type UserTotals struct {
	Login            string
	PRsCreated       int
	PRsMerged        int
	PRsReviewed      int
	CommentsGiven    int
	ApprovalsGiven   int
	ChangesRequested int
	LinesAdded       int
	LinesRemoved     int
}

// JiraUserStat holds aggregated Jira issue throughput for a single user.
type JiraUserStat struct {
	DisplayName   string
	CreatedCount  int
	FinishedCount int
}

// StatsData holds all stats data needed for rendering.
type StatsData struct {
	Days         []StatsDay
	TopReviewers []UserTotals
	TopAuthors   []UserTotals
	JiraYTD      []JiraUserStat
	JiraMonth    []JiraUserStat
}

// StatsLoader abstracts the store for loading contributor stats.
type StatsLoader interface {
	GetAllStats(ctx context.Context) ([]StatsDay, error)
	GetStatsByLogin(ctx context.Context, login string) ([]StatsDay, error)
	GetTopReviewers(ctx context.Context, n int) ([]UserTotals, error)
	GetTopAuthors(ctx context.Context, n int) ([]UserTotals, error)
	GetTeamMembers(ctx context.Context) ([]string, error)
	GetJiraUserStats(ctx context.Context, period string) ([]JiraUserStat, error)
}

// StatsProgressMsg is sent by the background goroutine to report fetch progress.
type StatsProgressMsg struct {
	Done    int
	Total   int
	Current string
}

// StatsReadyMsg signals that stats backfill is complete and data can be loaded.
type StatsReadyMsg struct{}

// statsDataLoadedMsg is sent when stats data has been loaded from the store.
type statsDataLoadedMsg struct {
	data  *StatsData
	users []string
	err   error
}

// statsViewMode toggles between team and individual user views.
type statsViewMode int

const (
	statsViewTeam statsViewMode = iota
	statsViewUser
)

// statsGranularity controls the sparkline time bucket size.
type statsGranularity int

const (
	statsGranWeekly statsGranularity = iota
	statsGranMonthly
)

// WithStatsLoader sets the StatsLoader implementation on the Model.
func WithStatsLoader(l StatsLoader) Option {
	return func(m *Model) {
		m.statsLoader = l
	}
}

// loadStatsData returns a tea.Cmd that fetches all stats data from the store.
func (m Model) loadStatsData() tea.Cmd {
	loader := m.statsLoader
	if loader == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()

		days, err := loader.GetAllStats(ctx)
		if err != nil {
			return statsDataLoadedMsg{err: err}
		}

		topReviewers, err := loader.GetTopReviewers(ctx, 10)
		if err != nil {
			return statsDataLoadedMsg{err: err}
		}

		topAuthors, err := loader.GetTopAuthors(ctx, 10)
		if err != nil {
			return statsDataLoadedMsg{err: err}
		}

		users, err := loader.GetTeamMembers(ctx)
		if err != nil {
			return statsDataLoadedMsg{err: err}
		}

		jiraYTD, _ := loader.GetJiraUserStats(ctx, "ytd")
		jiraMonth, _ := loader.GetJiraUserStats(ctx, "month")

		return statsDataLoadedMsg{
			data: &StatsData{
				Days:         days,
				TopReviewers: topReviewers,
				TopAuthors:   topAuthors,
				JiraYTD:      jiraYTD,
				JiraMonth:    jiraMonth,
			},
			users: users,
		}
	}
}
