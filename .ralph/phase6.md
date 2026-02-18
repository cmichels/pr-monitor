# Ralph Phase 6: Wezterm Integration + Main Entrypoint

> **Goal**: Wire everything together and provide wezterm config snippets.
> **Depends on**: Phase 5 (TUI)
> **Design Reference**: `plans/pr-monitor-final.md`

```json
[
  {
    "id": "6.1",
    "name": "Wezterm Lua integration snippets",
    "test_command": "test -f wezterm/pr-monitor.lua && test -f wezterm/README.md",
    "max_iterations": 5
  },
  {
    "id": "6.2",
    "name": "Main entrypoint wiring all packages",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./...",
    "max_iterations": 15
  }
]
```

---

## Task 6.1: Wezterm Lua Integration Snippets

Create ready-to-use Lua snippets for wezterm configuration: status bar badge
and auto-launch tab.

### Context

- Target directory: `wezterm/`
- Status JSON path: `~/.config/pr-monitor/status.json`
- The Go process writes this file; wezterm reads it
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Create `wezterm/pr-monitor.lua`:

```lua
-- pr-monitor wezterm integration
-- Add to your wezterm.lua: require("pr-monitor")
-- Or copy the relevant sections into your existing config.

local wezterm = require("wezterm")
local mux = wezterm.mux

local M = {}

-- Status bar badge: shows pending review count in the right-status area.
-- Reads ~/.config/pr-monitor/status.json written by the pr-monitor process.
function M.setup_status_bar(config)
  wezterm.on("update-right-status", function(window, pane)
    local home = os.getenv("HOME") or ""
    local status_path = home .. "/.config/pr-monitor/status.json"

    local success, content = pcall(function()
      local f = io.open(status_path, "r")
      if not f then return nil end
      local data = f:read("*a")
      f:close()
      return data
    end)

    if not success or not content or content == "" then
      return
    end

    local ok, json = pcall(wezterm.json_parse, content)
    if not ok or not json then
      return
    end

    local parts = {}

    if json.review_count and json.review_count > 0 then
      local review_color = "#4caf50"
      if json.oldest_review_age_hours and json.oldest_review_age_hours > 48 then
        review_color = "#f44336"
      elseif json.oldest_review_age_hours and json.oldest_review_age_hours > 24 then
        review_color = "#ff9800"
      elseif json.oldest_review_age_hours and json.oldest_review_age_hours > 4 then
        review_color = "#f9a825"
      end

      table.insert(parts, { Foreground = { Color = review_color } })
      table.insert(parts, { Text = string.format(" %d review%s ", json.review_count, json.review_count == 1 and "" or "s") })
    end

    if json.authored_activity_count and json.authored_activity_count > 0 then
      table.insert(parts, { Foreground = { Color = "#64b5f6" } })
      table.insert(parts, { Text = string.format(" %d update%s ", json.authored_activity_count, json.authored_activity_count == 1 and "" or "s") })
    end

    if #parts > 0 then
      window:set_right_status(wezterm.format(parts))
    else
      window:set_right_status("")
    end
  end)
end

-- Auto-launch: spawns pr-monitor in a dedicated tab on wezterm startup.
function M.setup_auto_launch(config)
  wezterm.on("gui-startup", function(cmd)
    local tab, pane, window = mux.spawn_window(cmd or {})
    -- Spawn pr-monitor in a second tab
    window:spawn_tab({ args = { "pr-monitor" } })
    -- Switch back to the first tab
    tab:activate()
  end)
end

-- Convenience: set up both status bar and auto-launch.
function M.setup(config)
  M.setup_status_bar(config)
  M.setup_auto_launch(config)
end

return M
```

2. Create `wezterm/README.md` with setup instructions:

- How to install the Lua module (symlink or copy to wezterm config dir)
- How to add to existing wezterm.lua
- What the status bar shows
- How to customize colors

### Success Criteria

1. [ ] `wezterm/pr-monitor.lua` implements status bar badge + auto-launch
2. [ ] Status bar reads `~/.config/pr-monitor/status.json`
3. [ ] Badge color changes based on oldest review age (shame timer)
4. [ ] Auto-launch spawns pr-monitor in a dedicated tab
5. [ ] `wezterm/README.md` has setup instructions
6. [ ] `test -f wezterm/pr-monitor.lua` passes
7. [ ] `test -f wezterm/README.md` passes

### Constraints

- Lua must be compatible with wezterm's built-in Lua runtime
- Use `pcall` for all I/O to avoid crashing wezterm on errors
- Status bar update should be fast (no blocking I/O, no external processes)
- Do NOT assume pr-monitor binary is in PATH — user may need to adjust the args

### Delegation Strategy

- **Delegate to subagents**: Read wezterm Lua API docs for `update-right-status`, `gui-startup`, `json_parse`
- **Keep in main context**: Write the Lua file and README

### Output

When all criteria are met, output: DONE

---

## Task 6.2: Main Entrypoint — Wire All Packages

Implement `cmd/pr-monitor/main.go` — the application entrypoint that wires
together config, auth, poller, store, notify, discover, and TUI.

### Context

- Target file: `cmd/pr-monitor/main.go` (exists, currently a stub)
- All internal packages are implemented in phases 1-5
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Implement the main function with this lifecycle:

```go
func main() {
    // 1. Parse flags (--help, --version, --config)
    // 2. Load config (LoadDefault or from flag)
    // 3. Resolve GitHub token (poller.ResolveToken)
    // 4. Validate token scopes (poller.ValidateScopes) — warn if missing
    // 5. Open SQLite store (store.New)
    // 6. Create poller (poller.NewPoller)
    // 7. Build repo discovery index (discover.NewIndex + Scan in background)
    // 8. Create notifier: open /dev/tty for OSC output, pass to notify.NewNotifier
    // 9. Create Bubble Tea program (but don't start yet — need the *tea.Program ref)
    //    program := tea.NewProgram(model, tea.WithAltScreen())
    // 10. Start poll loop in goroutine — pass program reference for refresh messages
    //    go pollLoop(ctx, poller, store, notifier, cfg, program)
    // 11. Start Bubble Tea TUI — program.Run() blocks until quit
    // 12. On TUI exit: cancel context, close /dev/tty, close store, cleanup
}
```

2. Poll loop goroutine:

```go
// pollLoop needs the *tea.Program reference to send refresh messages to the TUI.
func pollLoop(ctx context.Context, p *poller.Poller, s *store.Store, n *notify.Notifier, cfg *config.Config, program *tea.Program) {
    ticker := time.NewTicker(cfg.GitHub.PollInterval)
    defer ticker.Stop()

    // Run immediately on start, then on each tick
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
```

3. Single poll cycle:

```go
func poll(ctx context.Context, p *poller.Poller, s *store.Store, n *notify.Notifier, cfg *config.Config, program *tea.Program) {
    // Fetch review requests
    reviews, err := p.FetchReviewRequests(ctx)
    // Fetch authored PR activity
    authored, err := p.FetchAuthoredPRs(ctx)

    // Upsert all results into store
    // For authored PRs with activity, also call store.UpdateActivity

    // Collect current PR IDs for cleanup
    // Cleanup stale PRs not in current results

    // Find new items (notified_at IS NULL)
    // Emit toasts for new items
    // Mark as notified

    // Write status JSON
    // Get counts and oldest age from store
    // notify.WriteStatus(...)

    // IMPORTANT: Tell the TUI to refresh its data from the store.
    // tui.RefreshMsg is an exported type — the TUI's Update handler
    // will re-query the store and update the list views.
    program.Send(tui.RefreshMsg{})
}
```

4. Wire the TUI with concrete implementations:

```go
// The TUI interfaces map to our concrete types:
// tui.PRLoader   → store.Store (GetPendingByRole)
// tui.RepoResolver → discover.Index (Resolve)
// tui.Dismisser  → store.Store (Dismiss)

model := tui.New(store, discover, tui.ShameConfig{
    GreenHours:  cfg.ShameTimer.Green,
    YellowHours: cfg.ShameTimer.Yellow,
    RedHours:    cfg.ShameTimer.Red,
})
```

5. Signal handling:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Listen for SIGINT/SIGTERM
sigs := make(chan os.Signal, 1)
signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
go func() {
    <-sigs
    cancel()
}()
```

6. Flags:
- `--version` — print version and exit
- `--help` — print usage and exit
- `--config PATH` — override config file path

7. First-run experience:
- If config file doesn't exist: use defaults, print a message about creating config
- If `gh auth token` fails: print a clear error message with fix instructions
- If `read:org` scope is missing: print a warning but continue

### Success Criteria

1. [ ] `cmd/pr-monitor/main.go` wires all packages together
2. [ ] Config loading with fallback to defaults
3. [ ] Auth resolution with clear error messages
4. [ ] Poll loop runs on configurable interval
5. [ ] Poll cycle: fetch → upsert → cleanup → notify → write status
6. [ ] TUI receives data through interfaces
7. [ ] Graceful shutdown on SIGINT/SIGTERM
8. [ ] `--version`, `--help`, `--config` flags work
9. [ ] `go build -o /dev/null ./...` succeeds
10. [ ] `go vet ./...` is clean
11. [ ] `go test ./...` — all tests pass

### Constraints

- Use `flag` standard library for flag parsing — no third-party CLI frameworks
- The poll goroutine must respect context cancellation
- Store must be closed on shutdown (defer)
- Do NOT block the TUI with poll operations (poll runs in separate goroutine)
- The TUI should receive data refreshes via Bubble Tea messages, not direct store reads in the view

### Delegation Strategy

- **Delegate to subagents**: Read all internal/ package public APIs to understand what to wire
- **Keep in main context**: Write main.go

### Output

When all criteria are met, output: DONE
