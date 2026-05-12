# Architecture

`pr-monitor` is a terminal-native GitHub pull request monitoring system designed for fast review workflows and production-safe local operation.

## System Overview

The application runs as a single Go binary and coordinates six core modules:

- `internal/config` for YAML configuration loading and validation
- `internal/poller` for GitHub GraphQL polling and auth scope checks
- `internal/store` for SQLite persistence and state/delta tracking
- `internal/notify` for terminal toast notifications and status file output
- `internal/tui` for Bubble Tea interaction and operator workflows
- `internal/discover` for local repository/worktree resolution

## Data Flow

1. `poller` fetches review-request and authored-PR activity data from GitHub.
2. `store` writes or updates canonical PR state and computes deltas.
3. `notify` emits user-visible alerts only for new meaningful deltas.
4. `tui` reads from `store` and renders focused operator views.
5. `discover` maps repository IDs to local paths for one-key review launch.

## Storage Model

State is persisted in SQLite under `~/.config/pr-monitor/` to support:

- deduplication of known PR events
- dismissal/review status tracking
- authored PR activity timelines
- fast startup with historical context

This enables deterministic notifications and avoids noisy re-alerting.

## Auth and Security Model

- Authentication is resolved through `gh auth token` from the local GitHub CLI session.
- No static credentials are required in repository files.
- Scope validation warns when `read:org` is missing for team review discovery.

## UI and Workflow Design

The TUI is designed around reducing context-switching overhead:

- review queue visibility with age signals
- authored PR update visibility
- quick jump into local repo context for follow-up actions
- keyboard-first interactions suitable for tmux/terminal workflows

## Reliability Considerations

- polling and UI responsibilities are decoupled through shared persistence
- module boundaries favor testability and future daemon/client splits
- startup and shutdown paths are signal-aware and cancellation-safe

## Integration Surfaces

- GitHub GraphQL API for PR metadata and review events
- WezTerm status integration via `status.json`
- optional Jira integration for teams that pair GitHub with issue workflows
