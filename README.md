# pr-monitor

Terminal-native GitHub pull request monitoring with an interactive TUI, review workflow launch, and optional Jira context.

## Why This Exists

Review queues and authored PR updates are easy to miss across large orgs and multiple repositories. `pr-monitor` gives a single command-center view for:

- PRs that need your review (personal and team-requested)
- Activity on PRs you authored (approvals, comments, change requests)
- Fast context switching from signal to action

This project is built for engineers who want tight feedback loops and high ownership over delivery flow.

## Core Features

- Polls GitHub for review requests and authored PR activity
- Persists state in SQLite and detects deltas between polling cycles
- Bubble Tea TUI with focused workflow tabs and keyboard navigation
- Notification support via OSC terminal toasts
- WezTerm status integration (`wezterm/pr-monitor.lua`)
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

See `wezterm/README.md` for status bar and auto-launch integration.

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
