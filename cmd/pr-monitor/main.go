package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chrismichels/pr-monitor/internal/config"
	"github.com/chrismichels/pr-monitor/internal/detail"
	"github.com/chrismichels/pr-monitor/internal/discover"
	"github.com/chrismichels/pr-monitor/internal/notify"
	"github.com/chrismichels/pr-monitor/internal/poller"
	"github.com/chrismichels/pr-monitor/internal/stats"
	"github.com/chrismichels/pr-monitor/internal/store"
	"github.com/chrismichels/pr-monitor/internal/tui"
)

var version = "dev"

// defaultConfigYAML is the commented template written on first run.
const defaultConfigYAML = `# pr-monitor configuration
# See: https://github.com/chrismichels/pr-monitor

github:
  # GitHub organization (required if review_teams are set)
  org: ""
  # Teams whose review requests to monitor
  review_teams: []
  # How often to poll GitHub (Go duration string)
  poll_interval: "3m"

# Directories to scan for local git clones (used for review launch)
workspace_dirs:
  - "~/projects"

# Override auto-discovered repo paths: "org/repo" -> "/local/path"
repo_overrides: {}

# PR age color thresholds (in hours)
shame_timer:
  green: 4
  yellow: 24
  red: 48

notifications:
  toast_enabled: true
  status_json_path: "~/.config/pr-monitor/status.json"
`

func main() {
	var (
		showVersion bool
		configPath  string
		debug       bool
	)
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()

	// Set up structured logging to stderr (TUI owns stdout).
	setupLogging(debug)

	if showVersion {
		fmt.Println("pr-monitor", version)
		return
	}

	// First-run: ensure config directory and file exist.
	firstRun, err := ensureFirstRun()
	if err != nil {
		slog.Error("first-run setup failed", "error", err)
		os.Exit(1)
	}
	if firstRun {
		fmt.Fprintln(os.Stderr, "pr-monitor: first run detected")
		fmt.Fprintln(os.Stderr, "  Created config: ~/.config/pr-monitor/config.yaml")
		fmt.Fprintln(os.Stderr, "  Edit the config to set your GitHub org and review teams.")
	}

	// Load config.
	var cfg *config.Config
	if configPath != "" {
		cfg, err = config.Load(configPath)
	} else {
		cfg, err = config.LoadDefault()
	}
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Resolve GitHub token.
	token, err := poller.ResolveToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: could not resolve GitHub token.")
		fmt.Fprintln(os.Stderr, "Make sure the GitHub CLI is installed and authenticated:")
		fmt.Fprintln(os.Stderr, "  brew install gh")
		fmt.Fprintln(os.Stderr, "  gh auth login")
		os.Exit(1)
	}

	// Validate token scopes (warn only).
	if err := poller.ValidateScopes(token); err != nil {
		slog.Warn("token scope validation", "error", err)
		fmt.Fprintln(os.Stderr, "Team-based review requests may not work without read:org scope.")
		fmt.Fprintln(os.Stderr, "Run: gh auth refresh -s read:org")
	}

	// Open SQLite store.
	dbPath := resolveDBPath()
	st, err := store.New(dbPath)
	if err != nil {
		slog.Error("failed to open database", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Create poller.
	p, err := poller.NewPoller(token, cfg.GitHub.Org, cfg.GitHub.ReviewTeams)
	if err != nil {
		slog.Error("failed to create poller", "error", err)
		os.Exit(1)
	}

	// Build repo discovery index (scan runs in background).
	idx := discover.NewIndex(cfg.RepoOverrides)
	go func() {
		if err := idx.Scan(cfg.WorkspaceDirs); err != nil {
			slog.Warn("repo scan error", "error", err)
		}
	}()

	// Open /dev/tty for OSC toast notifications (Bubble Tea owns stdout).
	ttyFile, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		slog.Warn("could not open /dev/tty for notifications", "error", err)
	}
	var ttyWriter *os.File
	if err == nil {
		ttyWriter = ttyFile
		defer ttyWriter.Close()
	}

	notifier := notify.NewNotifier(ttyWriter, cfg.Notifications.ToastEnabled)

	// Context for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for SIGINT/SIGTERM.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	// Create detail fetcher for on-demand PR detail loading.
	detailFetcher := detail.NewFetcher(token)

	// Create the TUI model with adapter for store -> tui.PRLoader.
	adapter := &storeAdapter{store: st}
	sAdapter := &statsAdapter{store: st}
	shame := tui.ShameConfig{
		GreenHours:  cfg.ShameTimer.Green,
		YellowHours: cfg.ShameTimer.Yellow,
		RedHours:    cfg.ShameTimer.Red,
	}
	model := tui.New(adapter, idx, shame,
		tui.WithDismisser(st),
		tui.WithDetailFetcher(detailFetcher),
		tui.WithStatsLoader(sAdapter),
	)

	// Create Bubble Tea program.
	program := tea.NewProgram(model, tea.WithAltScreen())

	// Start poll loop in background goroutine.
	go pollLoop(ctx, p, st, notifier, cfg, program)

	// Start stats backfill loop in background goroutine.
	statsFetcher := stats.NewFetcher(token, cfg.GitHub.Org)
	go statsLoop(ctx, statsFetcher, st, cfg.GitHub.ReviewTeams, program)

	slog.Info("starting pr-monitor", "version", version)

	// Run TUI (blocks until quit).
	if _, err := program.Run(); err != nil {
		slog.Error("TUI error", "error", err)
		os.Exit(1)
	}
}

// setupLogging configures log/slog with a text handler writing to stderr.
func setupLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

// ensureFirstRun creates the config directory and default config file if they
// don't exist. Returns true if this was a first run (config was created).
func ensureFirstRun() (bool, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, fmt.Errorf("resolving home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}

	dir := filepath.Join(configDir, "pr-monitor")
	configFile := filepath.Join(dir, "config.yaml")

	// If config file already exists, not a first run.
	if _, err := os.Stat(configFile); err == nil {
		return false, nil
	}

	// Create directory.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating config directory: %w", err)
	}

	// Write default config.
	if err := os.WriteFile(configFile, []byte(defaultConfigYAML), 0o644); err != nil {
		return false, fmt.Errorf("writing default config: %w", err)
	}

	return true, nil
}

// resolveDBPath returns the SQLite database path under the config directory.
func resolveDBPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("failed to resolve home directory", "error", err)
			os.Exit(1)
		}
		configDir = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configDir, "pr-monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("failed to create data directory", "error", err)
		os.Exit(1)
	}
	return filepath.Join(dir, "pr-monitor.db")
}

// storeAdapter adapts store.Store to satisfy tui.PRLoader by converting
// store.PR to tui.PR.
type storeAdapter struct {
	store *store.Store
}

func (a *storeAdapter) GetPendingByRole(ctx context.Context, role string) ([]tui.PR, error) {
	prs, err := a.store.GetPendingByRole(ctx, role)
	if err != nil {
		return nil, err
	}
	result := make([]tui.PR, len(prs))
	for i, p := range prs {
		result[i] = storePRToTUI(p)
	}
	return result, nil
}

func storePRToTUI(p store.PR) tui.PR {
	tp := tui.PR{
		PRID:           p.PRID,
		Repo:           p.Repo,
		Number:         p.Number,
		Title:          p.Title,
		Author:         p.Author,
		URL:            p.URL,
		FilesChanged:   p.FilesChanged,
		CIStatus:       p.CIStatus,
		ReviewerStatus: p.ReviewerStatus,
		IsDraft:        p.IsDraft,
		FirstSeen:      p.FirstSeen,
		Status:         p.Status,
	}
	if p.LastActivityType != nil {
		tp.LastActivityType = *p.LastActivityType
	}
	if p.LastActivityBy != nil {
		tp.LastActivityBy = *p.LastActivityBy
	}
	if p.LastActivityAt != nil {
		tp.LastActivityAt = *p.LastActivityAt
	}
	return tp
}

// pollLoop runs the poll cycle on a configurable interval, sending refresh
// messages to the TUI after each cycle.
func pollLoop(ctx context.Context, p *poller.Poller, s *store.Store, n *notify.Notifier, cfg *config.Config, program *tea.Program) {
	ticker := time.NewTicker(cfg.GitHub.PollInterval)
	defer ticker.Stop()

	// Run immediately on start.
	poll(ctx, p, s, n, cfg, program)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll(ctx, p, s, n, cfg, program)
		}
	}
}

// poll executes a single poll cycle: fetch -> upsert -> cleanup -> notify -> status -> refresh TUI.
// Errors are logged and sent to the TUI as inline error messages.
func poll(ctx context.Context, p *poller.Poller, s *store.Store, n *notify.Notifier, cfg *config.Config, program *tea.Program) {
	slog.Debug("poll cycle starting")

	var currentPRIDs []string

	// Fetch review requests.
	reviews, err := p.FetchReviewRequests(ctx)
	if err != nil {
		handlePollError(err, "review requests", program)
	} else {
		slog.Debug("fetched review requests", "count", len(reviews))
		for _, r := range reviews {
			if err := s.UpsertPR(ctx, pollResultToStorePR(r)); err != nil {
				slog.Error("upsert review PR failed", "pr_id", r.PRID, "error", err)
			}
			currentPRIDs = append(currentPRIDs, r.PRID)
		}
	}

	// Fetch authored PR activity.
	authored, err := p.FetchAuthoredPRs(ctx)
	if err != nil {
		handlePollError(err, "authored PRs", program)
	} else {
		slog.Debug("fetched authored PRs", "count", len(authored))
		for _, a := range authored {
			if err := s.UpsertPR(ctx, pollResultToStorePR(a)); err != nil {
				slog.Error("upsert authored PR failed", "pr_id", a.PRID, "error", err)
			}
			if a.LastActivityAt != nil && a.LastActivityType != nil && a.LastActivityBy != nil {
				if err := s.UpdateActivity(ctx, a.PRID, *a.LastActivityAt, *a.LastActivityType, *a.LastActivityBy); err != nil {
					slog.Error("update activity failed", "pr_id", a.PRID, "error", err)
				}
			}
			currentPRIDs = append(currentPRIDs, a.PRID)
		}
	}

	// Cleanup stale PRs not in current results.
	if err := s.Cleanup(ctx, currentPRIDs); err != nil {
		slog.Error("cleanup stale PRs failed", "error", err)
	}

	// Notify for new review requests.
	newReviews, err := s.FindNew(ctx, "reviewer")
	if err != nil {
		slog.Error("find new review PRs failed", "error", err)
	} else {
		for _, pr := range newReviews {
			_ = n.NotifyNewReview(notify.PR{
				Repo:   pr.Repo,
				Number: pr.Number,
				Title:  pr.Title,
				Author: pr.Author,
			})
			_ = s.MarkNotified(ctx, pr.PRID)
		}
		if len(newReviews) > 0 {
			slog.Info("new review requests", "count", len(newReviews))
		}
	}

	// Notify for new authored PR activity.
	newAuthored, err := s.FindNew(ctx, "author")
	if err != nil {
		slog.Error("find new authored PRs failed", "error", err)
	} else {
		for _, pr := range newAuthored {
			activityType := ""
			activityBy := ""
			if pr.LastActivityType != nil {
				activityType = *pr.LastActivityType
			}
			if pr.LastActivityBy != nil {
				activityBy = *pr.LastActivityBy
			}
			_ = n.NotifyActivity(notify.PR{
				Repo:             pr.Repo,
				Number:           pr.Number,
				Title:            pr.Title,
				Author:           pr.Author,
				LastActivityType: activityType,
				LastActivityBy:   activityBy,
			})
			_ = s.MarkNotified(ctx, pr.PRID)
		}
		if len(newAuthored) > 0 {
			slog.Info("new authored PR activity", "count", len(newAuthored))
		}
	}

	// Write status JSON for wezterm.
	reviewCount, authoredCount, err := s.GetCounts(ctx)
	if err != nil {
		slog.Error("get PR counts failed", "error", err)
	} else {
		oldestAge, err := s.OldestPendingAge(ctx)
		if err != nil {
			slog.Error("get oldest pending age failed", "error", err)
			oldestAge = 0
		}
		_ = notify.WriteStatus(cfg.Notifications.StatusJSONPath, notify.Status{
			ReviewCount:           reviewCount,
			AuthoredActivityCount: authoredCount,
			LastUpdated:           time.Now(),
			OldestReviewAgeHours:  oldestAge.Hours(),
		})
	}

	// Tell the TUI to refresh its data from the store.
	program.Send(tui.RefreshMsg{})
	slog.Debug("poll cycle complete")
}

// handlePollError classifies a poll error and sends the appropriate message
// to the TUI. Logs all errors to slog.
func handlePollError(err error, source string, program *tea.Program) {
	var authErr *poller.AuthExpiredError
	var rlErr *poller.RateLimitError

	switch {
	case errors.As(err, &authErr):
		slog.Error("auth expired", "source", source, "error", err)
		program.Send(tui.PollErrorMsg{
			Text: "GitHub token expired -- run: gh auth login",
		})
	case errors.As(err, &rlErr):
		wait := time.Until(rlErr.ResetAt).Round(time.Second)
		slog.Warn("rate limited", "source", source, "reset_at", rlErr.ResetAt, "wait", wait)
		program.Send(tui.PollErrorMsg{
			Text: fmt.Sprintf("Rate limited -- next poll in %s", wait),
		})
	default:
		slog.Error("poll failed", "source", source, "error", err)
		program.Send(tui.PollErrorMsg{
			Text: "GitHub API unreachable -- retrying...",
		})
	}
}

// statsAdapter adapts store.Store to satisfy tui.StatsLoader by converting
// store types to tui types.
type statsAdapter struct {
	store *store.Store
}

func (a *statsAdapter) GetAllStats(ctx context.Context) ([]tui.StatsDay, error) {
	rows, err := a.store.GetAllStats(ctx)
	if err != nil {
		return nil, err
	}
	return convertDailyStats(rows), nil
}

func (a *statsAdapter) GetStatsByLogin(ctx context.Context, login string) ([]tui.StatsDay, error) {
	rows, err := a.store.GetStatsByLogin(ctx, login)
	if err != nil {
		return nil, err
	}
	return convertDailyStats(rows), nil
}

func (a *statsAdapter) GetTopReviewers(ctx context.Context, n int) ([]tui.UserTotals, error) {
	rows, err := a.store.GetTopReviewers(ctx, n)
	if err != nil {
		return nil, err
	}
	return convertUserTotals(rows), nil
}

func (a *statsAdapter) GetTopAuthors(ctx context.Context, n int) ([]tui.UserTotals, error) {
	rows, err := a.store.GetTopAuthors(ctx, n)
	if err != nil {
		return nil, err
	}
	return convertUserTotals(rows), nil
}

func (a *statsAdapter) GetTeamMembers(ctx context.Context) ([]string, error) {
	return a.store.GetTeamMembers(ctx)
}

func convertDailyStats(rows []store.DailyStat) []tui.StatsDay {
	result := make([]tui.StatsDay, len(rows))
	for i, r := range rows {
		result[i] = tui.StatsDay{
			Login:            r.Login,
			Date:             r.Date,
			PRsCreated:       r.PRsCreated,
			PRsMerged:        r.PRsMerged,
			PRsReviewed:      r.PRsReviewed,
			CommentsGiven:    r.CommentsGiven,
			LinesAdded:       r.LinesAdded,
			LinesRemoved:     r.LinesRemoved,
			ApprovalsGiven:   r.ApprovalsGiven,
			ChangesRequested: r.ChangesRequested,
		}
	}
	return result
}

func convertUserTotals(rows []store.UserTotal) []tui.UserTotals {
	result := make([]tui.UserTotals, len(rows))
	for i, r := range rows {
		result[i] = tui.UserTotals{
			Login:            r.Login,
			PRsCreated:       r.PRsCreated,
			PRsMerged:        r.PRsMerged,
			PRsReviewed:      r.PRsReviewed,
			CommentsGiven:    r.CommentsGiven,
			ApprovalsGiven:   r.ApprovalsGiven,
			ChangesRequested: r.ChangesRequested,
			LinesAdded:       r.LinesAdded,
			LinesRemoved:     r.LinesRemoved,
		}
	}
	return result
}

// statsLoop runs the stats backfill if needed, sending progress to the TUI.
func statsLoop(ctx context.Context, fetcher *stats.Fetcher, s *store.Store, teams []string, program *tea.Program) {
	needed, err := stats.BackfillNeeded(ctx, s)
	if err != nil {
		slog.Error("stats backfill check failed", "error", err)
		program.Send(tui.StatsReadyMsg{})
		return
	}

	if !needed {
		slog.Debug("stats backfill not needed")
		program.Send(tui.StatsReadyMsg{})
		return
	}

	slog.Info("starting stats backfill")
	onProgress := func(p stats.Progress) {
		program.Send(tui.StatsProgressMsg{
			Done:    p.Done,
			Total:   p.Total,
			Current: p.Current,
		})
	}

	if err := stats.RunBackfill(ctx, fetcher, s, teams, onProgress); err != nil {
		slog.Error("stats backfill failed", "error", err)
	}

	program.Send(tui.StatsReadyMsg{})
}

// pollResultToStorePR converts a poller.PollResult to a store.PR for upserting.
func pollResultToStorePR(r poller.PollResult) store.PR {
	return store.PR{
		PRID:           r.PRID,
		Repo:           r.Repo,
		Number:         r.Number,
		Title:          r.Title,
		Author:         r.Author,
		URL:            r.URL,
		Role:           r.Role,
		FilesChanged:   r.FilesChanged,
		CIStatus:       r.CIStatus,
		ReviewerStatus: r.ReviewerStatus,
		IsDraft:        r.IsDraft,
	}
}
