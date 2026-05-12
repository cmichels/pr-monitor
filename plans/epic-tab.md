# Epics Tab — Phase 1 Implementation Session

## Status: COMPLETE (Phase 1 + Phase 2 + Phase 3 + Phase 4)

All code compiles, all tests pass: `go build -o /dev/null ./... && go vet ./... && go test ./...`

---

## What Was Implemented

### New Files Created

#### `internal/tui/epic_cmd.go`
- `TrackedEpic` type (TUI's view of store.TrackedEpic)
- `EpicSection` struct (EpicKey + Name for each dynamic section)
- `EpicLoader` interface (`GetActiveTrackedEpics`, `GetJiraIssuesBySource`)
- `epicDataLoadedMsg` and `EpicRefreshMsg` message types
- `loadEpicData() tea.Cmd` — fetches active epics + child issues per epic
- `rebuildEpicLists(items)` — creates/resets epicLists and epicCollapsed slices, preserves collapse state
- `maybeLoadJiraDetailForEpic() tea.Cmd` — detail loading for selected epic item (reuses Jira detail cache)
- `SelectedEpicItem() (JiraItem, bool)` — returns selected item on tab 4

#### `internal/tui/epic_view.go`
- `renderEpicsTab(m Model) string` — iterates dynamic N sections, renders headers + lists
- `resizeEpicSections()` — distributes height among expanded sections (same pattern as resizeJiraSections but for dynamic N)
- Empty state: "No tracked epics. Add epics in config.yaml under jira.epics"

#### `internal/store/epic_test.go`
- `TestSyncTrackedEpics` — insert + update name
- `TestSyncTrackedEpics_PreservesActive` — deactivated epics survive re-sync
- `TestGetActiveTrackedEpics_Ordering` — sort by sort_order then key
- `TestCleanupJiraIssuesByPrefix` — only deletes epic-sourced issues
- `TestCleanupJiraIssuesByPrefix_Empty` — empty currentKeys deletes all epic issues
- `TestCleanupJiraIssuesNonEpic` — only deletes non-epic issues

#### `internal/tui/epic_test.go`
- `TestEpicsTab_Appears` — tab[4] == "Epics"
- `TestEpicDataLoadedMsg_PopulatesSections` — sections + lists populated correctly
- `TestEpicSectionSwitching` — 's' key cycles sections, wraps around
- `TestSelectedEpicItem` — returns false when not on tab 4, returns item when on tab 4

### Modified Files

#### `internal/store/jira.go`
- Added `tracked_epics` table to `createJiraSchema`:
  ```sql
  CREATE TABLE IF NOT EXISTS tracked_epics (
      epic_key TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '',
      active INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  ```
- `TrackedEpic` type: `{ EpicKey, Name string; Active bool; SortOrder int }`
- `SyncTrackedEpics(ctx, []TrackedEpic)` — INSERT OR IGNORE + UPDATE name/sort_order, never touches active
- `GetActiveTrackedEpics(ctx)` — WHERE active=1 ORDER BY sort_order, epic_key
- `CleanupJiraIssuesByPrefix(ctx, prefix, currentKeys)` — DELETE WHERE source LIKE prefix+'%' AND key NOT IN (...)
- `CleanupJiraIssuesNonEpic(ctx, currentKeys)` — DELETE WHERE source NOT LIKE 'epic:%' AND key NOT IN (...)

#### `internal/config/config.go`
- Added `JiraEpic` type: `{ Key, Name string }`
- Added `Epics []JiraEpic` field to `JiraConfig` and `rawJiraConfig`
- Updated `UnmarshalYAML` to pass Epics through

#### `internal/tui/model.go`
- New fields on Model: `epicLoader`, `epicSection`, `epicSections`, `epicLists`, `epicCollapsed`
- Tab 4 "Epics" added to `tabs` slice (now 5 tabs)
- `WithEpicLoader(l EpicLoader) Option`
- `Init()`: loads epic data if epicLoader != nil
- Filter state guard: excludes tab 4 from lists[] filtering, checks epicLists instead
- Tab 4 keybinding block: `s` (section switch), `x` (collapse), `r` (worktree), `o` (open), `y` (copy), `c` (claim), `l` (focus detail), cross-boundary j/k navigation
- Tab switch resets: `m.epicSection = 0`
- `handleTabSwitch()`: case 4 loads epic detail
- `WindowSizeMsg`: calls `resizeEpicSections()`
- `epicDataLoadedMsg` handler: rebuilds epicSections/epicLists/epicCollapsed, clamps epicSection
- `EpicRefreshMsg` handler: calls loadEpicData()
- List delegation: tab 4 routes to `epicLists[epicSection]` instead of `lists[activeListIndex()]`
- Detail panel focus 'r' handler: extended to tab 4

#### `internal/tui/view.go`
- Body switch: `case 4: listView = renderEpicsTab(m)`
- `showDetail` condition: includes `m.activeTab == 4 && m.jiraDetailFetcher != nil`
- `renderHeader()`: case 4 with total issue count across all epic lists
- `renderFooter()`: case 4 uses same hints as Jira tab
- Footer detail indicator: recognizes tab 4
- `renderHelpOverlay()`: added Epics tab section

#### `internal/tui/detail_view.go`
- `updateDetailViewport()`: changed `m.activeTab == 3` to `m.activeTab == 3 || m.activeTab == 4`

#### `cmd/pr-monitor/main.go`
- `epicAdapter` struct adapting store.Store to tui.EpicLoader
- Startup: seeds tracked epics from config via `st.SyncTrackedEpics()`
- Wires `tui.WithEpicLoader(epicAdapt)` into TUI opts
- `jiraPoll()`: fetches epic children (`parent = <key>` JQL), upserts with source `"epic:<key>"`
- Partitioned cleanup: replaced blanket `CleanupJiraIssues` with:
  1. `CleanupJiraIssuesNonEpic` for filter/my_tasks issues
  2. `CleanupJiraIssuesByPrefix("epic:", epicKeys)` for epic issues
- Sends `EpicRefreshMsg{}` after `JiraRefreshMsg{}`
- Updated `defaultConfigYAML` with commented epics example

#### `internal/tui/model_test.go`
- Updated `TestNew_InitialState`: expects 5 tabs, asserts tabs[4] == "Epics"
- Updated `TestTabSwitching`: accounts for tab 4 in forward/backward cycling

---

## Key Architecture Decision

Epic lists live in a **separate `epicLists []list.Model`** slice, NOT in the shared `lists[0..9]`. This avoids fragile index renumbering when adding/removing tracked epics. Parallel `epicSections`, `epicCollapsed` slices grow/shrink dynamically.

Tracked epics are stored in **SQLite** (tracked_epics table), seeded from config on startup. The `active` flag is never overwritten by config sync, enabling Phase 3's interactive toggle.

Epic child issues reuse the existing `jira_issues` table with `source = "epic:<key>"`. Cleanup is partitioned: epic and non-epic issues are cleaned up independently so neither poll cycle deletes the other's data.

---

## What's Next

### Phase 2: Stats Header per Epic — COMPLETE

**What was done:**
- `EpicStats` struct in `epic_cmd.go`: `Total, Open, Done, Blocked, Unassigned, MyCount`
- `computeEpicStats(items []JiraItem, currentUser string) EpicStats` — classifies by `StatusCat` ("done" vs open), detects "blocked" in status name (case-insensitive), counts unassigned and user-owned items
- `currentUser` field added to Model, populated from `jiraDataLoadedMsg` (inferred from my_tasks assignee)
- `epicStats []EpicStats` field on Model, computed in `epicDataLoadedMsg` handler
- `renderEpicSectionHeader()` in `epic_view.go` — enhanced header with colored progress bar (█/░), percentage, done/total, mine, blocked, unassigned counts
- `renderProgressBar(done, total, width)` — green filled (color 42) + dim empty (color 240)
- Stats visible even when sections are collapsed (dashboard-at-a-glance)
- Tests: `TestComputeEpicStats` (5 table-driven subtests), `TestEpicStats_PopulatedOnDataLoaded`, `TestRenderProgressBar`, `TestRenderEpicSectionHeader_ContainsStats`, `TestRenderEpicSectionHeader_CollapsedStillShowsStats`, `TestRenderEpicSectionHeader_EmptyEpic`

### Phase 3: Interactive Epic Management — COMPLETE

**What was done:**

Store methods (`internal/store/jira.go`):
- `AddTrackedEpic(ctx, key, name)` — INSERT with auto-incrementing sort_order via `COALESCE(MAX+1, 0)`, reactivates existing deactivated epics via ON CONFLICT
- `SetEpicActive(ctx, key, active)` — UPDATE active flag
- `RemoveTrackedEpic(ctx, key)` — transactional DELETE of epic + all child issues (`source = "epic:<key>"`)

TUI interface (`internal/tui/epic_cmd.go`):
- `EpicManager` interface: `AddTrackedEpic`, `SetEpicActive`, `RemoveTrackedEpic`
- `epicAddedMsg`, `epicToggledMsg`, `epicRemovedMsg` message types
- `addEpic()`, `toggleEpic()`, `removeEpic()` tea.Cmd factories

Model wiring (`internal/tui/model.go`):
- `epicManager EpicManager` field, `WithEpicManager` option
- `epicAddInput textinput.Model` + `epicAddActive bool` for inline text prompt
- `a` key: activates text input prompt (works even with no epics loaded)
- `t` key: toggles active→inactive on focused section (hides it)
- `D` key: removes focused section entirely (deletes epic + child issues)
- Enter submits, Esc cancels in text input mode
- `epicAddedMsg` handler: reloads data, shows status
- `epicToggledMsg` handler: reloads data, shows status
- `epicRemovedMsg` handler: reloads data, clamps section index, shows status
- All three keys are no-ops when `epicManager` is nil

View (`internal/tui/epic_view.go`):
- Text input prompt with "Add epic key:" label and Enter/Esc hints
- Empty state message updated: "Press 'a' to add one" when manager is available

Wiring (`cmd/pr-monitor/main.go`):
- `epicAdapter` now implements both `EpicLoader` and `EpicManager` (wraps store methods)
- `WithEpicManager(epicAdapt)` added to TUI opts

Help & footer (`internal/tui/view.go`):
- Help overlay: added a/t/D entries under Epics Tab section
- Footer: Epics tab shows `a:add | t:hide | D:remove`

Tests:
- Store: `TestAddTrackedEpic`, `TestAddTrackedEpic_ReactivatesExisting`, `TestSetEpicActive`, `TestRemoveTrackedEpic`, `TestAddTrackedEpic_SortOrder`
- TUI: `TestEpicAddPrompt_ActivatesOnA`, `TestEpicAddPrompt_EscCancels`, `TestEpicAddPrompt_EnterSubmits`, `TestEpicAddPrompt_EmptyInputIgnored`, `TestEpicToggle_HidesEpic`, `TestEpicRemove_RemovesEpic`, `TestEpicAddedMsg_ReloadsData`, `TestEpicRemovedMsg_ClampsSection`, `TestEpicNoManagerKeys_Ignored`

### Phase 4: Assignee Partitioning — COMPLETE

**What was done:**

Items within each epic section are now partitioned by assignee into three groups: **mine**, **unassigned**, and **others** (assigned to someone else). By default, only mine + unassigned items are shown. The `e` key toggles visibility of "others" items.

Core types (`internal/tui/epic_cmd.go`):
- `epicPartition` struct with `mine`, `unassigned`, `others` slices
- `allItems()` and `visibleItems()` methods for list construction
- `partitionEpicItems(items, currentUser)` — splits by assignee match
- `epicListItems(section)` — returns list items based on current showOthers state

Model state (`internal/tui/model.go`):
- `epicPartitions []epicPartition` — per-section partitioned data
- `epicShowOthers []bool` — per-section toggle (default false = hide others)
- `e` key handler toggles showOthers, rebuilds list items for that section
- Both states preserved across data refreshes

View (`internal/tui/epic_view.go`):
- Dim hint line below each section: "N assigned to others [e to show]" when hidden
- "showing all · N others [e to hide]" when visible
- Height calculation accounts for hint lines

Help/footer: `e:others` added to footer and help overlay

Tests:
- `TestPartitionEpicItems` — correct splitting with current user
- `TestPartitionEpicItems_NoCurrentUser` — all assigned go to "others"
- `TestEpicPartition_AllItems` — correct ordering mine→unassigned→others
- `TestEpicPartition_VisibleItems` — excludes others
- `TestEpicShowOthers_Toggle` — list count changes correctly on e press
- `TestEpicShowOthers_PreservedOnRefresh` — state survives data reload

---

## Config Example

```yaml
jira:
  base_url: "https://example.atlassian.net"
  epics:
    - key: "OP-3309"
      name: "UX/UI Design 2026"
    - key: "OP-4000"
      name: "Backend Rewrite"
```
