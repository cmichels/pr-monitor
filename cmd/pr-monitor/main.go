package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chrismichels/pr-monitor/internal/config"
	"github.com/chrismichels/pr-monitor/internal/detail"
	"github.com/chrismichels/pr-monitor/internal/discover"
	"github.com/chrismichels/pr-monitor/internal/jira"
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

# Jira integration (requires acli CLI: /opt/homebrew/bin/acli)
jira:
  base_url: ""
  poll_interval: "5m"
  filters: []
  # epics:
  #   - key: "OP-3309"
  #     name: "UX/UI Design 2026"
  # my_tasks_jql: 'project = OP AND assignee = currentUser() AND status IN ("To Do", "In Progress") ORDER BY updated DESC'
  # sprint_jql: 'project = OP AND sprint in openSprints() AND status IN ("To Do", "In Progress") ORDER BY status ASC, updated DESC'
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

	// Context for graceful shutdown — created before any network calls so startup is also cancellable.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for SIGINT/SIGTERM.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	// Open SQLite store.
	dbPath := resolveDBPath()
	st, err := store.New(dbPath)
	if err != nil {
		slog.Error("failed to open database", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Create poller.
	p, err := poller.NewPoller(ctx, token, cfg.GitHub.Org, cfg.GitHub.ReviewTeams, cfg.GitHub.ExcludeRepos)
	if err != nil {
		slog.Error("failed to create poller", "error", err)
		os.Exit(1)
	}

	// Build repo discovery index. Scan runs synchronously so the index is
	// fully populated before the TUI starts (avoids "repo not found" on fast Enter).
	idx := discover.NewIndex(cfg.RepoOverrides)
	if err := idx.Scan(cfg.WorkspaceDirs); err != nil {
		slog.Warn("repo scan error", "error", err)
	}

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

	// Create detail fetcher for on-demand PR detail loading.
	detailFetcher := &detailAdapter{fetcher: detail.NewFetcher(token)}

	// Create the TUI model with adapter for store -> tui.PRLoader.
	adapter := &storeAdapter{store: st}
	sAdapter := &statsAdapter{store: st}
	shame := tui.ShameConfig{
		GreenHours:  cfg.ShameTimer.Green,
		YellowHours: cfg.ShameTimer.Yellow,
		RedHours:    cfg.ShameTimer.Red,
	}

	opts := []tui.Option{
		tui.WithDismisser(st),
		tui.WithUndismisser(st),
		tui.WithDetailFetcher(detailFetcher),
		tui.WithStatsLoader(sAdapter),
	}

	// Wire up Jira integration if configured.
	var jiraClient *jira.Client
	if cfg.Jira.BaseURL != "" {
		acliPath, err := exec.LookPath("acli")
		if err != nil {
			acliPath = "/opt/homebrew/bin/acli" // macOS Homebrew fallback
		}
		jiraClient = jira.NewClient(acliPath)
		jiraAdapt := &jiraAdapter{store: st}
		jiraDetailAdapt := &jiraDetailAdapter{client: jiraClient}
		opts = append(opts,
			tui.WithJiraLoader(jiraAdapt),
			tui.WithJiraDetailFetcher(jiraDetailAdapt),
			tui.WithJiraClaimer(jiraClient),
			tui.WithJiraBaseURL(cfg.Jira.BaseURL),
			tui.WithJiraSourceKeys(jiraSourceKeysFromConfig(cfg)),
		)

		// Seed tracked epics from config and wire epic loader.
		if len(cfg.Jira.Epics) > 0 {
			seedEpics := make([]store.TrackedEpic, len(cfg.Jira.Epics))
			for i, e := range cfg.Jira.Epics {
				seedEpics[i] = store.TrackedEpic{
					EpicKey:   e.Key,
					Name:      e.Name,
					SortOrder: i,
				}
			}
			if err := st.SyncTrackedEpics(ctx, seedEpics); err != nil {
				slog.Error("failed to seed tracked epics", "error", err)
			}
		}
		epicAdapt := &epicAdapter{store: st}
		opts = append(opts, tui.WithEpicLoader(epicAdapt), tui.WithEpicManager(epicAdapt))

		// Wire sprint loader.
		sprintAdapt := &sprintAdapter{store: st}
		opts = append(opts, tui.WithSprintLoader(sprintAdapt))
	}

	model := tui.New(adapter, idx, shame, opts...)

	// Create Bubble Tea program.
	program := tea.NewProgram(model, tea.WithAltScreen())

	// Start poll loop in background goroutine.
	go pollLoop(ctx, p, st, notifier, cfg, program)

	// Start stats backfill loop in background goroutine.
	statsFetcher := stats.NewFetcher(token, cfg.GitHub.Org)
	go statsLoop(ctx, statsFetcher, st, cfg.GitHub.ReviewTeams, program)

	// Start Jira poll loop if configured.
	if jiraClient != nil {
		go jiraLoop(ctx, jiraClient, st, &cfg.Jira, program)
	}

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

// detailAdapter adapts detail.Fetcher to satisfy tui.DetailFetcher by
// converting detail.PRDetail to tui.PRDetail at the wiring boundary.
type detailAdapter struct {
	fetcher *detail.Fetcher
}

func (a *detailAdapter) FetchDetail(ctx context.Context, prNodeID string) (*tui.PRDetail, error) {
	d, err := a.fetcher.FetchDetail(ctx, prNodeID)
	if err != nil {
		return nil, err
	}
	result := &tui.PRDetail{
		Body: d.Body,
	}
	for _, f := range d.Files {
		result.Files = append(result.Files, tui.FileChange{
			Path:      f.Path,
			Additions: f.Additions,
			Deletions: f.Deletions,
		})
	}
	for _, c := range d.Checks {
		result.Checks = append(result.Checks, tui.Check{
			Name:       c.Name,
			Status:     c.Status,
			Conclusion: c.Conclusion,
		})
	}
	for _, c := range d.Comments {
		result.Comments = append(result.Comments, tui.Comment{
			Author:      c.Author,
			Body:        c.Body,
			CreatedAt:   c.CreatedAt,
			ReviewState: c.ReviewState,
		})
	}
	for _, r := range d.Reviews {
		result.Reviews = append(result.Reviews, tui.ReviewStatus{
			Author: r.Author,
			State:  r.State,
		})
	}
	return result, nil
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

func (a *storeAdapter) GetDismissedByRole(ctx context.Context, role string) ([]tui.PR, error) {
	prs, err := a.store.GetDismissedByRole(ctx, role)
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
	reviewsOK := false
	authoredOK := false

	// Fetch review requests.
	reviews, err := p.FetchReviewRequests(ctx)
	if err != nil {
		handlePollError(err, "review requests", program)
	} else {
		reviewsOK = true
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
		authoredOK = true
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

	// Only clean up stale PRs when both fetches succeeded.
	// A partial failure would give an incomplete ID set, silently deleting
	// valid PRs from the role whose fetch succeeded.
	if reviewsOK && authoredOK {
		if err := s.Cleanup(ctx, currentPRIDs); err != nil {
			slog.Error("cleanup stale PRs failed", "error", err)
		}
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
			if err := s.MarkNotified(ctx, pr.PRID); err != nil {
				slog.Error("mark notified failed", "pr_id", pr.PRID, "error", err)
			}
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
			if err := s.MarkNotified(ctx, pr.PRID); err != nil {
				slog.Error("mark notified failed", "pr_id", pr.PRID, "error", err)
			}
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

func (a *statsAdapter) GetJiraUserStats(ctx context.Context, period string) ([]tui.JiraUserStat, error) {
	rows, err := a.store.GetJiraUserStats(ctx, period)
	if err != nil {
		return nil, err
	}
	result := make([]tui.JiraUserStat, len(rows))
	for i, r := range rows {
		result[i] = tui.JiraUserStat{
			DisplayName:   r.DisplayName,
			CreatedCount:  r.CreatedCount,
			FinishedCount: r.FinishedCount,
		}
	}
	return result, nil
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

// statsLoop runs the stats backfill if needed, then re-checks every 24 hours.
func statsLoop(ctx context.Context, fetcher *stats.Fetcher, s *store.Store, teams []string, program *tea.Program) {
	runBackfillIfNeeded := func() {
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
			program.Send(tui.PollErrorMsg{Text: fmt.Sprintf("Stats backfill failed: %v", err)})
		}

		program.Send(tui.StatsReadyMsg{})
	}

	runBackfillIfNeeded()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runBackfillIfNeeded()
		}
	}
}

// jiraAdapter adapts store.Store to satisfy tui.JiraLoader.
type jiraAdapter struct {
	store *store.Store
}

func (a *jiraAdapter) GetJiraIssuesBySource(ctx context.Context, source string) ([]tui.JiraIssue, error) {
	issues, err := a.store.GetJiraIssuesBySource(ctx, source)
	if err != nil {
		return nil, err
	}
	result := make([]tui.JiraIssue, len(issues))
	for i, ji := range issues {
		result[i] = tui.JiraIssue{
			Key:       ji.Key,
			Summary:   ji.Summary,
			Status:    ji.Status,
			StatusCat: ji.StatusCat,
			Priority:  ji.Priority,
			IssueType: ji.IssueType,
			Assignee:  ji.Assignee,
			Reporter:  ji.Reporter,
			Source:    ji.Source,
			BrowseURL: ji.BrowseURL,
		}
	}
	return result, nil
}

// epicAdapter adapts store.Store to satisfy tui.EpicLoader.
type epicAdapter struct {
	store *store.Store
}

func (a *epicAdapter) GetActiveTrackedEpics(ctx context.Context) ([]tui.TrackedEpic, error) {
	epics, err := a.store.GetActiveTrackedEpics(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]tui.TrackedEpic, len(epics))
	for i, e := range epics {
		result[i] = tui.TrackedEpic{
			EpicKey:   e.EpicKey,
			Name:      e.Name,
			Active:    e.Active,
			SortOrder: e.SortOrder,
		}
	}
	return result, nil
}

func (a *epicAdapter) GetJiraIssuesBySource(ctx context.Context, source string) ([]tui.JiraIssue, error) {
	issues, err := a.store.GetJiraIssuesBySource(ctx, source)
	if err != nil {
		return nil, err
	}
	result := make([]tui.JiraIssue, len(issues))
	for i, ji := range issues {
		result[i] = tui.JiraIssue{
			Key:       ji.Key,
			Summary:   ji.Summary,
			Status:    ji.Status,
			StatusCat: ji.StatusCat,
			Priority:  ji.Priority,
			IssueType: ji.IssueType,
			Assignee:  ji.Assignee,
			Reporter:  ji.Reporter,
			Source:    ji.Source,
			BrowseURL: ji.BrowseURL,
		}
	}
	return result, nil
}

func (a *epicAdapter) AddTrackedEpic(ctx context.Context, epicKey, name string) error {
	return a.store.AddTrackedEpic(ctx, epicKey, name)
}

func (a *epicAdapter) SetEpicActive(ctx context.Context, epicKey string, active bool) error {
	return a.store.SetEpicActive(ctx, epicKey, active)
}

func (a *epicAdapter) RemoveTrackedEpic(ctx context.Context, epicKey string) error {
	return a.store.RemoveTrackedEpic(ctx, epicKey)
}

// sprintAdapter adapts store.Store to satisfy tui.SprintLoader.
type sprintAdapter struct {
	store *store.Store
}

func (a *sprintAdapter) GetJiraIssuesBySourcePrefix(ctx context.Context, prefix string) ([]tui.JiraIssue, error) {
	issues, err := a.store.GetJiraIssuesBySourcePrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	result := make([]tui.JiraIssue, len(issues))
	for i, ji := range issues {
		result[i] = tui.JiraIssue{
			Key:       ji.Key,
			Summary:   ji.Summary,
			Status:    ji.Status,
			StatusCat: ji.StatusCat,
			Priority:  ji.Priority,
			IssueType: ji.IssueType,
			Assignee:  ji.Assignee,
			Reporter:  ji.Reporter,
			Source:    ji.Source,
			BrowseURL: ji.BrowseURL,
		}
	}
	return result, nil
}

// jiraDetailAdapter adapts jira.Client to satisfy tui.JiraDetailFetcher.
type jiraDetailAdapter struct {
	client *jira.Client
}

func (a *jiraDetailAdapter) GetIssueDetail(_ context.Context, key string) (*tui.JiraDetail, error) {
	detail, err := a.client.GetIssueDetail(key)
	if err != nil {
		return nil, err
	}
	td := &tui.JiraDetail{
		Key:         detail.Key,
		Summary:     detail.Summary,
		Status:      detail.Status,
		Priority:    detail.Priority,
		IssueType:   detail.IssueType,
		Assignee:    detail.Assignee,
		Reporter:    detail.Reporter,
		Labels:      detail.Labels,
		Description: detail.Description,
	}
	for _, c := range detail.Comments {
		td.Comments = append(td.Comments, tui.JiraComment{
			Author:    c.Author,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	return td, nil
}

// jiraLoop polls Jira on a configurable interval, sending refresh messages to the TUI.
func jiraLoop(ctx context.Context, client *jira.Client, s *store.Store, cfg *config.JiraConfig, program *tea.Program) {
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	jiraPoll(ctx, client, s, cfg, program)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jiraPoll(ctx, client, s, cfg, program)
		}
	}
}

// jiraPoll executes a single Jira poll cycle.
func jiraPoll(ctx context.Context, client *jira.Client, s *store.Store, cfg *config.JiraConfig, program *tea.Program) {
	slog.Debug("jira poll cycle starting")

	filterKeySet := make(map[string]bool)
	var allKeys []string
	allFetchesOK := true

	// Fetch issues from each configured filter.
	for _, f := range cfg.Filters {
		issues, err := client.SearchByFilter(f.ID)
		if err != nil {
			slog.Error("jira filter search failed", "filter_id", f.ID, "error", err)
			allFetchesOK = false
			continue
		}
		source := fmt.Sprintf("filter:%d", f.ID)
		for _, issue := range issues {
			filterKeySet[issue.Key] = true
			allKeys = append(allKeys, issue.Key)
			if err := s.UpsertJiraIssue(ctx, jiraIssueToStore(issue, source, cfg.BaseURL)); err != nil {
				slog.Error("upsert jira issue failed", "key", issue.Key, "error", err)
			}
		}
		slog.Debug("fetched jira filter", "filter_id", f.ID, "count", len(issues))
	}

	// Fetch "my tasks" via JQL, excluding issues already in filters.
	if cfg.MyTasksJQL != "" {
		issues, err := client.SearchByJQL(cfg.MyTasksJQL)
		if err != nil {
			slog.Error("jira my_tasks search failed", "error", err)
			allFetchesOK = false
		} else {
			for _, issue := range issues {
				if filterKeySet[issue.Key] {
					continue
				}
				allKeys = append(allKeys, issue.Key)
				if err := s.UpsertJiraIssue(ctx, jiraIssueToStore(issue, "my_tasks", cfg.BaseURL)); err != nil {
					slog.Error("upsert jira my_task failed", "key", issue.Key, "error", err)
				}
			}
			slog.Debug("fetched jira my_tasks", "count", len(issues))
		}
	}

	// Only remove stale non-epic issues when all filter/my_tasks fetches succeeded.
	if allFetchesOK {
		if err := s.CleanupJiraIssuesNonEpic(ctx, allKeys); err != nil {
			slog.Error("cleanup non-epic jira issues failed", "error", err)
		}
	}

	// Fetch epic child issues for tracked epics.
	var epicKeys []string
	epicFetchesOK := true
	activeEpics, err := s.GetActiveTrackedEpics(ctx)
	if err != nil {
		slog.Error("get active tracked epics failed", "error", err)
		epicFetchesOK = false
	} else {
		for _, epic := range activeEpics {
			jql := fmt.Sprintf(`parent = %s ORDER BY status ASC, updated DESC`, epic.EpicKey)
			issues, err := client.SearchByJQL(jql)
			if err != nil {
				slog.Error("jira epic children search failed", "epic", epic.EpicKey, "error", err)
				epicFetchesOK = false
				continue
			}
			source := "epic:" + epic.EpicKey
			for _, issue := range issues {
				epicKeys = append(epicKeys, issue.Key)
				if err := s.UpsertJiraIssue(ctx, jiraIssueToStore(issue, source, cfg.BaseURL)); err != nil {
					slog.Error("upsert epic child issue failed", "key", issue.Key, "error", err)
				}
			}
			slog.Debug("fetched epic children", "epic", epic.EpicKey, "count", len(issues))
		}
	}

	// Cleanup stale epic issues separately.
	if epicFetchesOK {
		if err := s.CleanupJiraIssuesByPrefix(ctx, "epic:", epicKeys); err != nil {
			slog.Error("cleanup epic jira issues failed", "error", err)
		}
	}

	// Fetch sprint issues if sprint JQL is configured.
	if cfg.SprintJQL != "" {
		issues, err := client.SearchByJQL(cfg.SprintJQL)
		if err != nil {
			slog.Error("jira sprint search failed", "error", err)
		} else {
			// Get the active sprint name via the board API.
			sprintName := ""
			if cfg.Project != "" {
				name, err := client.GetActiveSprintName(cfg.Project)
				if err != nil {
					slog.Warn("could not get sprint name", "error", err)
				} else {
					sprintName = name
				}
			}

			source := "sprint"
			if sprintName != "" {
				source = "sprint:" + sprintName
			}
			var sprintKeys []string
			for _, issue := range issues {
				sprintKeys = append(sprintKeys, issue.Key)
				if err := s.UpsertJiraIssue(ctx, jiraIssueToStore(issue, source, cfg.BaseURL)); err != nil {
					slog.Error("upsert sprint issue failed", "key", issue.Key, "error", err)
				}
			}
			if err := s.CleanupJiraIssuesByPrefix(ctx, "sprint:", sprintKeys); err != nil {
				slog.Error("cleanup sprint issues failed", "error", err)
			}
			slog.Debug("fetched sprint issues", "sprint", sprintName, "count", len(issues))
		}
	}

	// Fetch Jira user stats if project is configured.
	if cfg.Project != "" {
		pollJiraStats(ctx, client, s, cfg.Project)
	}

	program.Send(tui.JiraRefreshMsg{})
	program.Send(tui.EpicRefreshMsg{})
	program.Send(tui.SprintRefreshMsg{})
	slog.Debug("jira poll cycle complete")
}

// jiraIssueToStore converts a jira.Issue to a store.JiraIssue.
func jiraIssueToStore(issue jira.Issue, source, baseURL string) store.JiraIssue {
	labels := "[]"
	if len(issue.Labels) > 0 {
		// Simple JSON array construction.
		parts := make([]string, len(issue.Labels))
		for i, l := range issue.Labels {
			parts[i] = fmt.Sprintf("%q", l)
		}
		labels = "[" + joinStrings(parts, ",") + "]"
	}
	return store.JiraIssue{
		Key:       issue.Key,
		Summary:   issue.Summary,
		Status:    issue.Status,
		StatusCat: issue.StatusCat,
		Priority:  issue.Priority,
		IssueType: issue.IssueType,
		Assignee:  issue.Assignee,
		Reporter:  issue.Reporter,
		Labels:    labels,
		Source:    source,
		BrowseURL: jira.BrowseURL(baseURL, issue.Key),
	}
}

// jiraSourceKeysFromConfig builds the [3]string source keys for the TUI Jira
// sections from the config's filter list. The first two filters map to
// "filter:<ID>" and the third slot is always "my_tasks".
func jiraSourceKeysFromConfig(cfg *config.Config) [3]string {
	keys := [3]string{"", "", "my_tasks"}
	for i, f := range cfg.Jira.Filters {
		if i >= 2 {
			break
		}
		keys[i] = fmt.Sprintf("filter:%d", f.ID)
	}
	return keys
}

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// pollJiraStats runs JQL queries to gather issue throughput stats and saves them to the store.
func pollJiraStats(ctx context.Context, client *jira.Client, s *store.Store, project string) {
	now := time.Now()
	yearStart := fmt.Sprintf("%d-01-01", now.Year())
	monthStart := fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())

	type query struct {
		jql   string
		field func(jira.Issue) string
	}

	ytdQueries := []query{
		{fmt.Sprintf(`project = %s AND created >= "%s"`, project, yearStart), func(i jira.Issue) string { return i.Reporter }},
		{fmt.Sprintf(`project = %s AND resolved >= "%s"`, project, yearStart), func(i jira.Issue) string { return i.Assignee }},
	}
	monthQueries := []query{
		{fmt.Sprintf(`project = %s AND created >= "%s"`, project, monthStart), func(i jira.Issue) string { return i.Reporter }},
		{fmt.Sprintf(`project = %s AND resolved >= "%s"`, project, monthStart), func(i jira.Issue) string { return i.Assignee }},
	}

	saveStats := func(period string, queries []query) {
		var createdMap, finishedMap map[string]int
		for qi, q := range queries {
			issues, err := client.SearchByJQL(q.jql)
			if err != nil {
				slog.Error("jira stats query failed", "period", period, "query_idx", qi, "error", err)
				return
			}
			agg := aggregateByField(issues, q.field)
			if qi == 0 {
				createdMap = agg
			} else {
				finishedMap = agg
			}
		}

		// Merge into a unified user list.
		users := make(map[string]bool)
		for u := range createdMap {
			users[u] = true
		}
		for u := range finishedMap {
			users[u] = true
		}

		var stats []store.JiraUserStat
		for u := range users {
			if u == "" {
				continue
			}
			stats = append(stats, store.JiraUserStat{
				DisplayName:   u,
				Period:        period,
				CreatedCount:  createdMap[u],
				FinishedCount: finishedMap[u],
			})
		}

		if err := s.ReplaceJiraUserStats(ctx, period, stats); err != nil {
			slog.Error("save jira user stats failed", "period", period, "error", err)
		} else {
			slog.Debug("saved jira user stats", "period", period, "users", len(stats))
		}
	}

	saveStats("ytd", ytdQueries)
	saveStats("month", monthQueries)
}

// aggregateByField counts occurrences grouped by a field extracted from each issue.
func aggregateByField(issues []jira.Issue, field func(jira.Issue) string) map[string]int {
	counts := make(map[string]int)
	for _, issue := range issues {
		key := field(issue)
		counts[key]++
	}
	return counts
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
