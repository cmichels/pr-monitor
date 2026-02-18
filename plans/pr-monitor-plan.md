# PR Monitor — Project Plan

## Vision
A terminal-native PR monitoring service that watches GitHub for PRs requiring review, notifies the user within wezterm, and enables a seamless keyboard-driven workflow to jump into a repo and execute `/review-pr`.

## Status: Discovery Phase

---

## Core Requirements (Known)
- Monitor GitHub for PRs where the user is a requested reviewer
- **Monitor PRs the user authored** — notify on approvals, comments, and change requests
- Notify within the terminal / wezterm (no browser context-switching)
- Enable quick "jump to repo + review" workflow
- Keyboard-centric — minimal mouse interaction
- Stay in the terminal ecosystem (wezterm + nvim + zsh + claude-code)

## Tech Stack (Tentative)
- **Language**: Go (aligns with user's primary backend language)
- **GitHub API**: `gh` CLI auth + GitHub REST/GraphQL API
- **Terminal UI**: TBD (Bubble Tea, plain CLI, or wezterm Lua integration)
- **Notifications**: TBD (OSC notifications, wezterm status bar, or TUI)

## Architecture (High-Level Sketch)
```
┌─────────────┐     poll/GraphQL      ┌──────────────┐
│   GitHub     │ ◄──────────────────── │  Go Daemon   │
│   API        │ ─────────────────────►│  (poller)    │
└─────────────┘     PR data            └──────┬───────┘
                                              │
                                    ┌─────────┼─────────┐
                                    │         │         │
                              ┌─────▼──┐ ┌────▼───┐ ┌──▼──────────┐
                              │ State  │ │ Notify │ │ Wezterm     │
                              │ Store  │ │ Engine │ │ Integration │
                              └────────┘ └────────┘ └─────────────┘
```

## Review Workflow (Target UX)
```
1. PR arrives needing review
2. User is notified (method TBD)
3. User triggers action (keybinding TBD)
4. Wezterm opens new pane/tab in correct repo directory
5. claude-code launches
6. /review-pr <number> executes
```

---

## Open Questions

### Q1: Scope of Monitoring
- All orgs/repos vs. curated list?
- Include @mentions in comments or just formal review requests?
- **Decision**: **Formal review requests across all accessible repos (no exclude list needed for MVP)**
  - Monitor PRs where user is directly requested as reviewer
  - Monitor PRs where `tsp-admin-contributors` team is requested
  - Monitor PRs where `tsp-contributors` team is requested
  - User is a CODEOWNER on most repos, so these teams capture the bulk of reviews
  - GitHub GraphQL: use `reviewRequests` union type (covers both User + Team)
  - Equivalent search queries: `review-requested:@me`, `team-review-requested:ORG/tsp-admin-contributors`, `team-review-requested:ORG/tsp-contributors`
  - v2: consider adding @mention scanning in PR comments
  - **Also monitor authored PRs** for activity:
    - Approved → toast: "PR #42 approved by @reviewer"
    - Commented → toast: "New comment on PR #42 by @reviewer"
    - Changes requested → toast: "Changes requested on PR #42 by @reviewer"
    - GitHub GraphQL: `author:@me is:open` + `timelineItems` for new activity
    - TUI shows two sections/tabs: "To Review" and "My PRs"

### Q2: Local Repo Availability
- Assume repos are cloned locally?
- Auto-clone if missing?
- **Decision**: **Auto-discover local repos + explicit overrides + prompted clone**
  - On startup, scan workspace directories (e.g. `/Volumes/data/projects/`) for repos
  - Build a map of GitHub remote URL → local path
  - Handle both regular clones (`.git/` directory) and worktrees (`.git` file)
  - Config supports explicit overrides: `repo → specific local path`
  - When a PR arrives for an unknown repo: **prompt the user** with options:
    - Clone to default workspace dir
    - Open in browser instead
    - Dismiss / skip
  - Most repos already cloned; this covers the edge cases gracefully

### Q3: Notification Style
- Passive (badge/counter) vs. Active (toast) vs. Both?
- **Decision**: **Both — Wezterm status bar badge + OSC toast for new arrivals**
  - Wezterm right-status bar shows persistent count: `[3 PRs pending review]`
  - Wezterm Lua reads state from a JSON file written by the Go daemon
  - OSC 9/777 toast notification fires only on **new** PRs (delta from last poll)
  - Toast includes: PR title, repo name, PR number, author
  - No repeated notifications for the same PR across poll cycles

### Q4: State Persistence
- Track reviewed/dismissed PRs?
- Storage mechanism?
- **Decision**: **SQLite file-based state**
  - Location: `~/.config/pr-monitor/state.db`
  - Schema tracks per-PR: `pr_id, repo, number, title, author, first_seen, last_seen, notified_at, status (pending|dismissed|reviewed), role (reviewer|author)`
  - For authored PRs: track `last_activity_at` + `activity_type (approved|commented|changes_requested)` to detect new events
  - Enables delta detection for toast notifications (only fire on new PRs)
  - Supports "dismiss" action — removes from badge count without declining on GitHub
  - Pure-Go SQLite driver (`modernc.org/sqlite`) — no CGo dependency
  - Enables future features: wait time tracking, priority scoring, review history

### Q5: Integration Depth
- Notify only vs. show PR metadata before review?
- **Decision**: **Summary view with triage capability (Option B)**
  - TUI list shows: repo, PR number, title, author, files changed, CI status, age
  - User can triage from the list: select to review, dismiss, or skip
  - One keypress from list → launches wezterm pane + claude-code + /review-pr
  - Metadata already in SQLite from polling — display is cheap
  - v2: expand to rich preview with diff stats, PR description, inline comments

### Q6: Deployment Model
- Background daemon vs. manual start vs. persistent TUI?
- **Decision**: **Wezterm auto-launch in dedicated tab (Option C)**
  - Wezterm Lua config spawns `pr-monitor` in a dedicated tab on startup
  - Single process: polls GitHub, updates SQLite, renders TUI, fires toasts
  - Wezterm status bar reads state JSON written by the process
  - Always running when terminal is open — zero manual effort
  - **Future (v2)**: Split into launchd daemon + on-demand TUI client (Option D)
    - Design internals with separation in mind: poller + TUI as distinct packages
    - Enables notifications even without terminal open
    - TUI becomes thin client reading shared SQLite

### Q7: Authentication
- Reuse `gh` CLI auth vs. separate PAT?
- **Decision**: **Reuse `gh` CLI auth (Option A)**
  - Shell out to `gh auth token` or read `~/.config/gh/hosts.yml`
  - Zero additional setup — already authenticated
  - Validate `read:org` scope on first run (needed for team review request queries)
  - v2: add PAT fallback for edge cases (CI, remote machines, `gh` not installed)

---

## Wild Ideas (To Evaluate)
- [ ] Priority scoring for PRs — **v2** (MVP: sort by age, oldest first)
- [ ] Review streak / gamification — **v2** (MVP: shame timer — color-coded age column with urgency thresholds)
- [x] Sound alerts — **Rejected** (OSC toast is sufficient; no audio)
- [x] Slack/Discord cross-reference — **Rejected** (org uses Teams; not applicable)

---

## Decisions Log
| # | Question | Decision | Date |
|---|----------|----------|------|
| — | Language | Go | 2026-02-18 |
| 1 | Scope of Monitoring | Formal review requests (personal + tsp-admin-contributors + tsp-contributors teams). All repos, no excludes for MVP. Also monitor authored PRs for approvals, comments, change requests. | 2026-02-18 |
| 2 | Local Repo Availability | Auto-discover repos in workspace dirs + config overrides. Prompt user to clone/skip/browser for unknown repos. Worktree-aware scanning. | 2026-02-18 |
| 3 | Notification Style | Both: wezterm status bar badge (persistent count) + OSC toast (new PRs only, delta-based). | 2026-02-18 |
| 4 | State Persistence | SQLite at ~/.config/pr-monitor/state.db. Tracks per-PR state for delta detection, dismiss, review history. Pure-Go driver. | 2026-02-18 |
| 5 | Integration Depth | Summary list view (repo, title, author, files, CI, age) with triage actions. One key to launch review. Rich preview in v2. | 2026-02-18 |
| 6 | Deployment Model | Wezterm auto-launch in dedicated tab (single process). Design for future daemon+TUI split (Option D). | 2026-02-18 |
| 7 | Authentication | Reuse `gh` CLI auth token. Validate read:org scope on first run. PAT fallback in v2. | 2026-02-18 |

## Next Steps
- [ ] Resolve open questions through interactive Q&A
- [ ] Finalize architecture based on decisions
- [ ] Define MVP scope
- [ ] Implementation plan with phases
