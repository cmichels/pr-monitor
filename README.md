# pr-monitor

Terminal-native GitHub pull request monitoring with an interactive TUI, tmux-first review workflows, and optional Jira context.

## Why This Exists

I built `pr-monitor` to keep my entire engineering workflow inside the terminal, where I already do all of my work. Running in a dedicated tmux tab, it gives me one keyboard-first control plane for team PRs, my PRs, Jira epics/boards, assigned Jira issues, team Git stats, and Claude session lifecycle (start/stop/resume). Instead of context-switching across browser tabs and tools, I can kick off reviews, resolve PR feedback, assign Jira work, and launch Claude-driven worktree flows directly from the terminal. The result is less workflow friction, faster execution, and tighter feedback loops without leaving my core environment.

`pr-monitor` provides a single command-center view for:

- PRs that need your review (personal and team-requested)
- Activity on PRs you authored (approvals, comments, change requests)
- Fast context switching from signal to action

This project is built for engineers who want tight feedback loops and high ownership over delivery flow.

## Core Features

- Polls GitHub for review requests and authored PR activity
- Persists state in SQLite and detects deltas between polling cycles
- Bubble Tea TUI with focused workflow tabs and keyboard navigation
- Notification support via OSC terminal toasts
- tmux-first review launch workflows with fast context switching
- WezTerm status integration (`wezterm/pr-monitor.lua`) for optional tab/status UX
- Repository auto-discovery for fast review launch into local clones/worktrees
- Optional Jira-backed context for teams that use Jira workflows

## Architecture Snapshot

- `cmd/pr-monitor/`: entrypoint and runtime wiring
- `internal/config/`: YAML config parsing and validation
- `internal/poller/`: GitHub GraphQL polling + auth/scope checks
- `internal/store/`: SQLite persistence and delta/state management
- `internal/tui/`: Bubble Tea model/view/update loop
- `internal/notify/`: terminal toast + status file writing
- `internal/discover/`: local repo/worktree discovery and path resolution

Design principle: polling, storage, and UI are decoupled to keep the system modular and testable.

For a deeper design walkthrough, see `docs/architecture.md`.

## Tech Stack

- Go
- Bubble Tea + Lip Gloss
- GitHub GraphQL API (`githubv4`)
- SQLite (`modernc.org/sqlite`)
- YAML config

## Quick Start

### Prerequisites

- Go 1.25+
- GitHub CLI (`gh`) authenticated for the target account/org
- `tmux` for full review workflow support

### Build

```bash
go build -o pr-monitor ./cmd/pr-monitor
```

### First Run

```bash
./pr-monitor
```

On first run, a config file is created at:

`~/.config/pr-monitor/config.yaml`

Update it with your org/team/workspace settings.

## Terminal Workflow

`pr-monitor` is designed for a keyboard-first terminal workflow and expects to run inside tmux for review launch functionality.

Example flow:

```bash
tmux new -s dev
pr-monitor
```

Use TUI keybindings to launch review windows and jump directly into local repo context.

## Terminal Compatibility

- Primary workflow: tmux-based terminal sessions
- Notification path: OSC terminal toasts (tmux passthrough supported)
- Typical setup: Ghostty + tmux
- Optional: WezTerm integration for status/tab enhancements

### Verify

```bash
go build -o /dev/null ./... && go vet ./... && go test ./...
```

## Configuration

Use `config.example.yaml` as your reference template. The app supports:

- GitHub org and review team configuration
- Polling intervals
- Workspace discovery directories
- Repo path overrides
- Notification toggles
- Optional Jira settings

## Security Model

- Authentication uses `gh auth token` from your local GitHub CLI session
- No hardcoded credentials required in source control
- Team-review support validates required GitHub scopes (`read:org`)

## WezTerm Integration

See `wezterm/README.md` for optional status bar and tab integration.

## Project Governance

- Security policy: `SECURITY.md`
- Contribution guide: `CONTRIBUTING.md`
- Architecture notes: `docs/architecture.md`

## Roadmap (Near-Term)

- Additional filters and sort controls in TUI
- Improved dashboarding on review throughput and aging
- Expanded notification customization

## License

License to be added.
