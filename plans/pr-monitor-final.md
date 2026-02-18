# PR Monitor — Final Plan

> A terminal-native GitHub PR monitoring tool that tracks review requests and authored PR activity, surfaces them in a Bubble Tea TUI inside wezterm, and enables one-keypress jumps into claude-code `/review-pr`.

**Date**: 2026-02-18
**Language**: Go
**Status**: Ready for implementation

---

## Table of Contents
- [MVP Scope](#mvp-scope)
- [Architecture](#architecture)
- [Tech Stack](#tech-stack)
- [Data Model](#data-model)
- [Configuration](#configuration)
- [TUI Design](#tui-design)
- [Notification System](#notification-system)
- [Wezterm Integration](#wezterm-integration)
- [Implementation Roadmap](#implementation-roadmap)
- [v2 Backlog](#v2-backlog)
- [Decisions Log](#decisions-log)

---

## MVP Scope

### What it does
1. **Polls GitHub** for PRs requiring user's review (personal + team-based requests)
2. **Polls GitHub** for activity on PRs the user authored (approvals, comments, change requests)
3. **Persists state** in SQLite — tracks seen/new/dismissed PRs, detects deltas
4. **Renders a TUI** (Bubble Tea) with two views: "To Review" and "My PRs"
5. **Fires OSC toast notifications** when new PRs or new activity arrives (delta-based)
6. **Writes status JSON** for wezterm status bar badge (persistent pending count)
7. **Launches review workflow** — one keypress opens wezterm pane → repo dir → claude-code → `/review-pr <number>`
8. **Auto-discovers local repos** — scans workspace dirs, handles worktrees, prompts for missing repos
9. **Shame timer** — color-coded age column (green → yellow → red) for aging review requests

### What it does NOT do (yet)
- Priority scoring / weighted sort
- Review streak tracking / stats dashboard
- Rich PR preview (diff stats, description, inline comments)
- Daemon mode (launchd) with separate TUI client
- @mention scanning in PR comments
- PAT fallback auth
- Audio notifications

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        pr-monitor                            │
│                    (single binary)                            │
│                                                              │
│  cmd/pr-monitor/main.go                                      │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                    Application                          │ │
│  │                                                         │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │ │
│  │  │ internal/ │  │ internal/ │  │ internal/ │  │internal│ │ │
│  │  │ poller   │  │ store    │  │ notify   │  │ tui    │ │ │
│  │  │          │  │          │  │          │  │        │ │ │
│  │  │ GitHub   │─►│ SQLite   │─►│ OSC toast│  │ Bubble │ │ │
│  │  │ GraphQL  │  │ state.db │  │ status   │  │ Tea    │ │ │
│  │  │ polling  │  │ CRUD     │  │ JSON     │  │ views  │ │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └────────┘ │ │
│  │                                                         │ │
│  │  ┌──────────┐  ┌──────────┐                             │ │
│  │  │ internal/ │  │ internal/ │                             │ │
│  │  │ discover │  │ config   │                             │ │
│  │  │          │  │          │                             │ │
│  │  │ repo     │  │ YAML     │                             │ │
│  │  │ scanner  │  │ loader   │                             │ │
│  │  └──────────┘  └──────────┘                             │ │
│  └─────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
         │                                    │
         ▼                                    ▼
  ~/.config/pr-monitor/               wezterm Lua config
  ├── config.yaml                     reads status.json
  ├── state.db                        renders badge in
  └── status.json                     right-status bar
```

### Package Responsibilities

| Package | Responsibility | Key Dependencies |
|---------|---------------|-----------------|
| `cmd/pr-monitor` | Entrypoint, wires everything together | — |
| `internal/poller` | GitHub GraphQL polling on interval, returns PR data | `github.com/shurcooL/githubv4` |
| `internal/store` | SQLite CRUD, delta detection, state queries | `modernc.org/sqlite` |
| `internal/notify` | OSC toast emission, status.json writer | — (stdout escape codes) |
| `internal/tui` | Bubble Tea model/view/update, keybindings, actions | `github.com/charmbracelet/bubbletea` |
| `internal/discover` | Scan filesystem for git repos, resolve remotes, worktree-aware | — (os/filepath, git config parsing) |
| `internal/config` | Load/validate YAML config | `gopkg.in/yaml.v3` |

> **Design note**: The `poller` and `tui` packages have no direct dependency on each other. They communicate through `store`. This enables the future daemon+TUI split (v2) with no refactoring — just different entrypoints wiring different packages.

---

## Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Primary backend language, fast compilation, single binary |
| GitHub API | GraphQL via `githubv4` | Fetch both review requests and authored PRs in minimal queries |
| Auth | `gh auth token` (reuse gh CLI) | Zero setup, validate `read:org` scope on first run |
| State DB | SQLite via `modernc.org/sqlite` | Pure-Go, no CGo, single file, battle-tested |
| TUI | Bubble Tea + Lip Gloss | De facto Go TUI stack, rich styling, composable |
| Config | YAML | Simple, human-readable, familiar |
| Notifications | OSC 9/777 escape sequences | Native wezterm support, no external dependencies |

---

## Data Model

### SQLite Schema: `pull_requests`

```sql
CREATE TABLE pull_requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id           TEXT NOT NULL UNIQUE,        -- GitHub node ID (globally unique)
    repo            TEXT NOT NULL,                -- "org/repo-name"
    number          INTEGER NOT NULL,             -- PR number
    title           TEXT NOT NULL,
    author          TEXT NOT NULL,                -- PR author login
    url             TEXT NOT NULL,                -- HTML URL for browser fallback
    role            TEXT NOT NULL,                -- "reviewer" | "author"
    files_changed   INTEGER DEFAULT 0,
    ci_status       TEXT DEFAULT 'unknown',       -- "passing" | "failing" | "pending" | "unknown"
    first_seen      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notified_at     DATETIME,                     -- when toast was last fired
    status          TEXT NOT NULL DEFAULT 'pending', -- "pending" | "dismissed" | "reviewed"

    -- authored PR activity tracking
    last_activity_at   DATETIME,
    last_activity_type TEXT,                      -- "approved" | "commented" | "changes_requested"
    last_activity_by   TEXT,                      -- login of who performed the action

    UNIQUE(repo, number)
);

CREATE INDEX idx_pr_role_status ON pull_requests(role, status);
CREATE INDEX idx_pr_repo ON pull_requests(repo);
```

### Status JSON: `~/.config/pr-monitor/status.json`

Written by the process after each poll cycle. Read by wezterm Lua.

```json
{
  "review_count": 3,
  "authored_activity_count": 2,
  "last_updated": "2026-02-18T14:30:00Z",
  "oldest_review_age_hours": 72
}
```

---

## Configuration

### File: `~/.config/pr-monitor/config.yaml`

```yaml
# GitHub settings
github:
  # Teams that trigger review monitoring (in addition to personal requests)
  review_teams:
    - "tsp-admin-contributors"
    - "tsp-contributors"
  # Org name for team-based queries
  org: "tsp-org"    # TODO: confirm actual org name
  # Poll interval
  poll_interval: "3m"

# Workspace directories to scan for local repos
workspace_dirs:
  - "/Volumes/data/projects"

# Explicit repo → local path overrides (optional)
repo_overrides:
  # "org/repo": "/path/to/local/clone"

# Clone settings
clone:
  default_dir: "/Volumes/data/projects"
  prompt_on_missing: true   # ask before cloning

# Shame timer thresholds (hours)
shame_timer:
  green: 4      # < 4 hours: green
  yellow: 24    # 4-24 hours: yellow
  red: 48       # > 48 hours: red

# Notification settings
notifications:
  toast_enabled: true
  status_json_path: "~/.config/pr-monitor/status.json"
```

---

## TUI Design

### Layout

```
╭─ PR Monitor ─────────────────────────────────── ? help ─╮
│                                                          │
│  [Tab: To Review (3)]  [Tab: My PRs (2)]                │
│                                                          │
│  To Review                                     sorted ↑  │
│  ┌──────────────────────────────────────────────────────┐│
│  │ repo           #     title              CI   age     ││
│  │─────────────────────────────────────────────────────  ││
│  │ api-gateway    #142  Fix auth refresh   ✓    2h      ││
│  │ frontend       #89   Add dark mode      ✓    1d      ││
│  │ infra          #201  Bump Go 1.23       ✗    3d      ││
│  └──────────────────────────────────────────────────────┘│
│                                                          │
│  r: review  d: dismiss  o: open in browser  q: quit     │
╰──────────────────────────────────────────────────────────╯
```

```
╭─ PR Monitor ─────────────────────────────────── ? help ─╮
│                                                          │
│  [Tab: To Review (3)]  [Tab: My PRs (2)]                │
│                                                          │
│  My PRs                                                  │
│  ┌──────────────────────────────────────────────────────┐│
│  │ repo           #     title              status       ││
│  │─────────────────────────────────────────────────────  ││
│  │ core-lib       #77   Refactor DB pool   ✅ approved  ││
│  │ api            #95   Add rate limit     💬 commented ││
│  └──────────────────────────────────────────────────────┘│
│                                                          │
│  o: open in browser  enter: jump to repo  q: quit       │
╰──────────────────────────────────────────────────────────╯
```

### Keybindings

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch between "To Review" / "My PRs" |
| `j` / `k` or `↑` / `↓` | Navigate list |
| `r` or `Enter` | Launch review (To Review tab): open wezterm pane → repo → claude-code → `/review-pr` |
| `Enter` | Jump to repo (My PRs tab): open wezterm pane in repo dir |
| `d` | Dismiss PR (removes from list, marks dismissed in SQLite) |
| `o` | Open PR in browser |
| `R` | Force refresh (poll now) |
| `?` | Toggle help overlay |
| `q` | Quit |

### Shame Timer Colors

| Age | Color | Indicator |
|-----|-------|-----------|
| < 4 hours | Green | Normal — fresh |
| 4–24 hours | Yellow | Aging — should review soon |
| 24–48 hours | Orange | Stale — someone's waiting |
| > 48 hours | Red | Overdue — drop everything |

---

## Notification System

### OSC Toast (new events only)

Emitted via stdout escape sequences when delta detection finds new items:

**New review request:**
```
\033]9;PR Review: api-gateway #142 — Fix auth refresh (by @dev)\033\\
```

**Authored PR activity:**
```
\033]9;PR Approved: core-lib #77 — Refactor DB pool (by @reviewer)\033\\
```

Toast fires once per new event. The `notified_at` / `last_activity_at` fields in SQLite prevent re-notification.

### Status JSON (continuous)

Written to `~/.config/pr-monitor/status.json` after every poll. Wezterm Lua reads this file to render the status bar badge.

---

## Wezterm Integration

### Status Bar Badge (Lua snippet for wezterm.lua)

```lua
-- pr-monitor status bar integration
wezterm.on("update-right-status", function(window, pane)
  local success, content = pcall(function()
    local f = io.open(os.getenv("HOME") .. "/.config/pr-monitor/status.json", "r")
    if not f then return nil end
    local data = f:read("*a")
    f:close()
    return data
  end)

  if success and content then
    local json = wezterm.json_parse(content)
    if json and json.review_count > 0 then
      local badge = string.format(" 📋 %d reviews ", json.review_count)
      window:set_right_status(wezterm.format({
        { Foreground = { Color = "#f9a825" } },
        { Text = badge },
      }))
    end
  end
end)
```

### Auto-launch Tab (Lua snippet)

```lua
-- spawn pr-monitor in a dedicated tab on startup
local launch_menu = {
  { label = "PR Monitor", args = { "pr-monitor" } },
}

config.launch_menu = launch_menu

-- or auto-spawn on startup:
wezterm.on("gui-startup", function(cmd)
  local tab, pane, window = mux.spawn_window({})
  -- spawn pr-monitor in a second tab
  window:spawn_tab({ args = { "pr-monitor" } })
  -- switch back to first tab
  tab:activate()
end)
```

### Review Launch Action

When user presses `r` on a PR in the TUI, the tool:

1. Resolves repo → local path (via discover index or config override)
2. If not found: prompt clone/browser/dismiss
3. If found: emit wezterm CLI command to spawn a new pane:
   ```bash
   wezterm cli split-pane --cwd /path/to/repo -- claude -p "/review-pr <PR_NUMBER>"
   ```
4. User lands in a claude-code session with the review running

---

## Implementation Roadmap

### Phase 1: Foundation (scaffold + config + auth)
> Get a binary that compiles, loads config, and authenticates with GitHub.

- [ ] `go mod init github.com/user/pr-monitor`
- [ ] Project structure: `cmd/pr-monitor/`, `internal/` packages
- [ ] `internal/config` — YAML loader with defaults and validation
- [ ] Auth — resolve `gh auth token`, validate scopes
- [ ] Basic CLI entrypoint that loads config and prints auth status
- [ ] Unit tests for config loading

### Phase 2: GitHub Poller
> Fetch real PR data from GitHub.

- [ ] `internal/poller` — GraphQL queries for:
  - PRs where user is requested reviewer
  - PRs where teams (`tsp-admin-contributors`, `tsp-contributors`) are requested reviewers
  - Open PRs authored by user with recent activity (reviews, comments)
- [ ] Polling loop with configurable interval
- [ ] Parse GraphQL responses into internal PR model
- [ ] Unit tests with mock GraphQL responses

### Phase 3: State Store
> Persist PR data, detect deltas.

- [ ] `internal/store` — SQLite schema creation and migrations
- [ ] CRUD operations: upsert PRs, update status, query by role/status
- [ ] Delta detection: compare poll results against stored state, return new/changed items
- [ ] Mark dismissed / reviewed
- [ ] Garbage collection: remove PRs that are no longer open (merged/closed)
- [ ] Unit tests with in-memory SQLite

### Phase 4: Notifications
> Toast alerts and status JSON.

- [ ] `internal/notify` — OSC 9 toast emission for new review requests
- [ ] OSC toast for authored PR activity (approved/commented/changes requested)
- [ ] Status JSON writer — write counts + metadata after each poll
- [ ] Delta-gated: only toast on new items (check `notified_at`)
- [ ] Integration test: poll → store → notify pipeline

### Phase 5: Repo Discovery
> Find local repos and map them to GitHub remotes.

- [ ] `internal/discover` — scan workspace directories recursively
- [ ] Parse `.git/config` for remote URLs (regular clones)
- [ ] Handle worktrees: detect `.git` file, resolve parent repo remote
- [ ] Build and cache repo → local path index
- [ ] Config override support (explicit repo path mappings)
- [ ] Prompt flow for missing repos (clone / browser / dismiss)

### Phase 6: TUI
> The interactive terminal UI.

- [ ] `internal/tui` — Bubble Tea application model
- [ ] Two-tab layout: "To Review" / "My PRs"
- [ ] Table rendering with Lip Gloss styling
- [ ] Shame timer: color-coded age column with configurable thresholds
- [ ] Keybindings: navigate, switch tabs, dismiss, open browser, force refresh
- [ ] Review launch action: resolve repo path → `wezterm cli split-pane` → claude-code
- [ ] Help overlay (`?` key)
- [ ] Wire TUI to poller (background goroutine) and store

### Phase 7: Wezterm Integration
> Status bar badge and auto-launch.

- [ ] Provide wezterm Lua snippets for status bar badge
- [ ] Provide wezterm Lua snippet for auto-launch tab on startup
- [ ] Test status.json reading from Lua
- [ ] Document wezterm config setup in README

### Phase 8: Polish & Ship
> Hardening, edge cases, documentation.

- [ ] Graceful shutdown (context cancellation, SQLite close)
- [ ] Error handling: GitHub rate limits, network failures, expired auth
- [ ] First-run experience: validate auth, create config dir, scan repos
- [ ] `--version`, `--help` flags
- [ ] Makefile / Taskfile for build, test, install
- [ ] README with setup instructions

---

## v2 Backlog

| Feature | Description |
|---------|-------------|
| **Priority scoring** | Weighted sort by age, repo priority, PR size, CI status, author |
| **Review streak / stats** | Track review cadence, avg response time, daily streak counter |
| **Rich PR preview** | Expandable rows with diff stats, PR description, inline comments |
| **Daemon + TUI split** | launchd daemon (`pr-monitord`) for background polling; TUI as thin client |
| **PAT fallback auth** | Config-based PAT for environments without `gh` CLI |
| **@mention scanning** | Detect informal review requests in PR comment bodies |
| **Exclude list** | Config to mute specific repos from monitoring |
| **Multi-org support** | Monitor across multiple GitHub orgs |

---

## Decisions Log

| # | Question | Decision | Date |
|---|----------|----------|------|
| — | Language | Go | 2026-02-18 |
| 1 | Scope of Monitoring | Formal review requests (personal + tsp-admin-contributors + tsp-contributors). Also monitor authored PRs for approvals, comments, change requests. | 2026-02-18 |
| 2 | Local Repo Availability | Auto-discover in workspace dirs + config overrides. Prompt for missing repos. Worktree-aware. | 2026-02-18 |
| 3 | Notification Style | Wezterm status bar badge (persistent) + OSC toast (delta-based, new items only). | 2026-02-18 |
| 4 | State Persistence | SQLite at ~/.config/pr-monitor/state.db. Tracks per-PR state, role, activity. Pure-Go driver. | 2026-02-18 |
| 5 | Integration Depth | Summary list view with triage actions. One key to launch review. | 2026-02-18 |
| 6 | Deployment Model | Wezterm auto-launch in dedicated tab (single process). Future: daemon + TUI split. | 2026-02-18 |
| 7 | Authentication | Reuse `gh` CLI auth token. Validate read:org scope on first run. | 2026-02-18 |
| 8 | Sort Order | By age, oldest first (shame-driven development). | 2026-02-18 |
| 9 | Shame Timer | MVP — color-coded age column (green/yellow/orange/red). | 2026-02-18 |
| 10 | Sound Alerts | Rejected — OSC toast is sufficient. | 2026-02-18 |
| 11 | Slack/Discord | Rejected — org uses Teams. | 2026-02-18 |
