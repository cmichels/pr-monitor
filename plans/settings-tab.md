# Settings Tab — Implementation Plan

## Status: COMPLETE

All code compiles, all tests pass: `go build -o /dev/null ./... && go vet ./... && go test ./...`

---

## Motivation

pr-monitor currently has no way to re-discover repos or trigger maintenance actions from within the TUI. If a user clones a new repo after pr-monitor starts, the only recourse is restarting the process. A Settings tab provides a home for operational actions (rescan, refresh) and can later host configurable toggles.

---

## Design

### Tab Position & Identity
- New tab at index 6: `"Settings"` (after Sprint)
- No item counts in the tab header — just the label
- No detail panel — Settings is a full-width tab (like Stats)
- No spinner — actions give instant status feedback via `statusMsg`

### UI Layout
The tab renders a **selectable action list** — a vertical menu of labeled actions the user navigates with `j/k` and triggers with `enter`. Each action has a name and a brief description rendered dimly to the right.

```
  Settings

  > Rescan Repos          Re-discover local git repos from workspace_dirs
    Force Refresh PRs     Fetch latest PR data from GitHub
    Force Refresh Jira    Fetch latest Jira data
    Force Refresh Epics   Reload epic issues
    Force Refresh Sprint  Reload sprint issues
```

The `>` cursor indicates the selected action. Arrow keys / j/k move the cursor. Enter executes.

### Design Decisions (deviations from original plan)

1. **Rescanner interface segregation** — instead of widening `RepoResolver` with `Rescan()`, a separate `Rescanner` interface is defined. The Settings tab type-asserts to check capability. Keeps `RepoResolver` clean.

2. **Dynamic action list** — actions are built in `New()` based on which loaders/resolvers are configured. If Jira isn't configured, "Force Refresh Jira" doesn't appear. If the resolver doesn't implement `Rescanner`, "Rescan Repos" doesn't appear.

3. **Atomic Rescan** — `Rescan()` builds a complete new map via `scanDirInto()`, then swaps under a single write lock. No window where `Resolve()` sees an empty map.

4. **Generation counter integration** — all refresh actions in executeSettingsAction() increment the per-source gen counter before dispatching loads, consistent with the staleness guard pattern.

5. **Cursor preserved across tab switches** — consistent with other tabs that preserve section/cursor position.

---

## What Was Implemented

### Phase 1: discover.Index Rescan

**`internal/discover/discover.go`**
- Added `dirs []string` field to `Index`
- `Scan()` stores dirs under write lock before walking
- `scanDirInto()` extracted from `scanDir()` — accepts a target map for atomic rescan
- `Rescan()` reads dirs under read lock, walks into new map, swaps atomically under write lock

**`internal/discover/discover_test.go`**
- `TestRescan_PicksUpNewRepo` — scan, add repo, rescan, verify found
- `TestRescan_NoDirsIsNoop` — no panic when dirs empty

### Phase 2-5: TUI Settings Tab

**`internal/tui/settings_cmd.go`** (new)
- `Rescanner` interface (separate from `RepoResolver`)
- `settingsAction` struct with stable `id` field
- `buildSettingsActions()` — dynamic based on configured loaders
- `rescanRepos()` tea.Cmd with type assertion
- `executeSettingsAction()` — dispatches with proper gen increments + loading flags

**`internal/tui/settings_view.go`** (new)
- `renderSettingsTab()` — cursor-based action list with highlight styling
- Height-padded to prevent layout collapse

**`internal/tui/model.go`**
- `settingsCursor int`, `settingsActions []settingsAction` fields
- Tab list extended to 7 tabs
- Settings keybindings: j/k/enter in `activeTab == 6` block
- Filtering guard excludes tab 6
- `rescanReposMsg` handler
- `buildSettingsActions()` called after options applied in `New()`

**`internal/tui/view.go`**
- Full-width body rendering for tab 6 (like Stats)
- Header: just the label, no counts
- Footer: `tab:switch | j/k:navigate | enter:execute | ?:help | q:quit`
- Help overlay: Settings section added
- Detail panel excluded for tab 6

### Phase 6: Tests

**`internal/tui/model_test.go`**
- `mockRescanResolver` implementing both `RepoResolver` + `Rescanner`
- `TestSettingsTab_Appears`
- `TestSettingsTab_ActionsBuiltDynamically`
- `TestSettingsTab_NoRescanWithoutRescanner`
- `TestSettingsCursor_Navigation` (wrapping both directions)
- `TestSettingsAction_RescanRepos`
- `TestSettingsAction_RefreshPRs`
- `TestSettingsTab_ViewRenders`
- `TestRescanReposMsg_Success`
- `TestRescanReposMsg_Error`
- Updated `TestNew_InitialState` (7 tabs)
- Updated `TestTabSwitching` (wraps through Settings)

---

## Tab Index Reference

| Index | Tab | Lists Used |
|-------|-----|-----------|
| 0 | To Review | lists[0] pending, lists[1] reviewed, lists[8] dismissed |
| 1 | My PRs | lists[2] active, lists[3] drafts, lists[9] dismissed |
| 2 | Stats | statsViewport (full-width) |
| 3 | Jira | lists[4-7] (in-progress, submissions, knowledge, my tasks) |
| 4 | Epics | epicLists[] (dynamic) |
| 5 | Sprint | sprintList |
| 6 | Settings | None — static action list via settingsCursor |

---

## What This Does NOT Do

- No config file editing from the TUI (future consideration)
- No toggle options yet (notifications, poll intervals) — the action list pattern supports adding these later as a separate section
- No "already scanning" guard — rapid enter presses could trigger concurrent rescans (low risk, rescan is fast)

---

## Verification

```
go build -o /dev/null ./... && go vet ./... && go test ./...
```
