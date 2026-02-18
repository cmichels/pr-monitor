# pr-monitor

## Ralph Loop Context

This project is being developed using Ralph Wiggum Loops.
- Task definitions: .ralph/phase*.md
- Guardrails: .ralph/guardrails.md (READ FIRST)
- Design reference: plans/pr-monitor-final.md
- Loop runner: plans/wiggum.md

## What This Tool Does

pr-monitor is a terminal-native GitHub PR monitoring tool. It:
1. Polls GitHub for PRs requiring user's review (personal + team-based)
2. Polls GitHub for activity on PRs the user authored (approvals, comments, change requests)
3. Persists state in SQLite, detects new events via delta comparison
4. Renders a Bubble Tea TUI with two tabs: "To Review" and "My PRs"
5. Fires OSC toast notifications for new events
6. Writes status JSON for wezterm status bar badge
7. Launches review workflow: wezterm pane → repo → claude-code → /review-pr

## Package Map

- `cmd/pr-monitor/` — entrypoint, wires everything
- `internal/config/` — YAML config loading
- `internal/poller/` — GitHub GraphQL polling + auth
- `internal/store/` — SQLite persistence + delta detection
- `internal/notify/` — OSC toast + status JSON writer
- `internal/tui/` — Bubble Tea interactive UI
- `internal/discover/` — local repo filesystem scanner (worktree-aware)

## Architecture Rule

**Poller and TUI do NOT import each other.** They communicate through Store (shared SQLite).
This enables a future split into separate daemon + TUI binaries.

Package dependency flow:
```
config ← (all packages read config)
poller → store (writes poll results)
store ← tui (reads for display)
store ← notify (reads for delta detection)
discover ← tui (resolves repo paths for review launch)
```

## Key Technical Decisions

- **SQLite**: Use `modernc.org/sqlite` (pure Go, no CGo). NOT `mattn/go-sqlite3`.
- **GitHub API**: GraphQL via `github.com/shurcooL/githubv4`. NOT REST.
- **Auth**: Shell out to `gh auth token`. NOT env vars or PAT files.
- **TUI**: `github.com/charmbracelet/bubbletea` + `lipgloss`.
- **Config**: YAML at `~/.config/pr-monitor/config.yaml` via `gopkg.in/yaml.v3`.
- **Notifications**: OSC 9 escape codes (`\033]9;msg\033\\`) to /dev/tty (NOT stdout — Bubble Tea owns stdout).

## GitHub Queries

Review requests (3 queries combined + deduplicated):
- `is:open is:pr review-requested:@me`
- `is:open is:pr team-review-requested:{org}/tsp-admin-contributors`
- `is:open is:pr team-review-requested:{org}/tsp-contributors`

Authored PR activity:
- `is:open is:pr author:@me` + `timelineItems` for review/comment events

## Verification

Always run before committing:
```
go build -o /dev/null ./... && go vet ./... && go test ./...
```

## Testing

- Use `github.com/stretchr/testify/assert` for assertions
- Use table-driven tests where applicable
- SQLite tests use in-memory database (`:memory:`)
- Mock GitHub API responses for poller tests (do NOT make real API calls in tests)
- Do NOT use testify suites unless shared setup is genuinely needed
