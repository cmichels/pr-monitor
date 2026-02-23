# Plan: Add Draft PRs Section to My PRs Tab

## Summary

Add a "Drafts" section to the "My PRs" tab, mirroring the Pending/Reviewed stacked section pattern already used in the "To Review" tab. This requires plumbing `isDraft` from GitHub GraphQL all the way through to the TUI.

## Current State

- **Poller:** `authored.go:25` query uses `-is:draft`, actively excluding drafts
- **Store:** No `is_draft` column in `pull_requests` table
- **Structs:** No `IsDraft` field on `PollResult`, `store.PR`, or `tui.PR`
- **TUI:** "My PRs" tab renders a single flat `list.Model` (`lists[2]`), no sections

## Changes by Layer

### 1. Poller — `internal/poller/authored.go` + `types.go`

**authored.go:**
- Remove `-is:draft` from the search query (line 25) so drafts are included in results
- Add `IsDraft githubv4.Boolean` field to the GraphQL struct (on the PullRequest fragment)
- Set `result.IsDraft = bool(pr.IsDraft)` when building PollResult

**types.go:**
- Add `IsDraft bool` field to `PollResult` struct

### 2. Store — `internal/store/store.go`

**Schema migration:**
- Add `ALTER TABLE pull_requests ADD COLUMN is_draft INTEGER DEFAULT 0` in `New()` alongside the existing `reviewer_status` migration

**PR struct:**
- Add `IsDraft bool` to `store.PR`

**UpsertPR:**
- Add `is_draft` to INSERT column list and ON CONFLICT UPDATE set
- Pass `pr.IsDraft` (convert bool → int for SQLite)

**GetPendingByRole:**
- Add `is_draft` to SELECT column list
- Scan into `pr.IsDraft` (scan as int, convert to bool)

### 3. Adapter — `cmd/pr-monitor/main.go`

**pollResultToStorePR:**
- Map `r.IsDraft` → `store.PR.IsDraft`

**storePRToTUI:**
- Map `store.PR.IsDraft` → `tui.PR.IsDraft`

### 4. TUI — `internal/tui/`

**model.go — PR struct:**
- Add `IsDraft bool` to `tui.PR`

**model.go — Model struct:**
- Add a 4th `list.Model` to `m.lists` (index 3) for draft PRs
  - `lists[2]` = Active authored PRs, `lists[3]` = Draft authored PRs
- Add section state fields:
  ```go
  myPRsSection      int  // 0=active focused, 1=drafts focused
  activeCollapsed   bool
  draftsCollapsed   bool
  ```

**model.go — activeListIndex():**
- Update tab 1 routing:
  ```go
  if m.activeTab == 1 {
      return 2 + m.myPRsSection  // 2=active, 3=drafts
  }
  ```

**model.go — New():**
- Create 4th list.Model (draftList) with same config as authorList
- Append to `m.lists`

**model.go — Update() — prsLoadedMsg handler:**
- Split `msg.authoredPRs` into active vs draft based on `IsDraft`:
  ```go
  var activeItems, draftItems []PRItem
  for _, item := range msg.authoredPRs {
      if item.pr.IsDraft {
          draftItems = append(draftItems, item)
      } else {
          activeItems = append(activeItems, item)
      }
  }
  m.lists[2].SetItems(toListItems(activeItems))
  m.lists[3].SetItems(toListItems(draftItems))
  ```

**model.go — Update() — key handling:**
- Extend `SectionSwitch` (`s` key) to also work on tab 1:
  ```go
  case key.Matches(msg, m.keys.SectionSwitch):
      if m.activeTab == 0 {
          m.reviewSection = 1 - m.reviewSection
      } else if m.activeTab == 1 {
          m.myPRsSection = 1 - m.myPRsSection
      }
  ```
- Extend `CollapseToggle` (`x` key) for tab 1
- Add cross-section `j`/`k` boundary navigation for tab 1 (same pattern as tab 0)

**model.go — resizing:**
- Add `resizeMyPRsSections()` method (parallel to `resizeStackedLists`) that splits height between lists[2] and lists[3]
- Call it from `WindowSizeMsg` handler (replace the single `lists[2].SetSize` call)

**model.go — windowTitle:**
- Update to include active:draft counts: `PR(pending:reviewed:active:draft)`

**view.go — renderView:**
- Replace `m.lists[2].View()` with `renderMyPRsSections(m)` when `activeTab == 1`

**view.go — new renderMyPRsSections():**
- Mirror `renderStackedSections` but for Active + Drafts:
  ```go
  func renderMyPRsSections(m Model) string {
      activeHeader := renderSectionHeader("Active", len(m.lists[2].Items()), m.myPRsSection == 0, m.activeCollapsed)
      draftsHeader := renderSectionHeader("Drafts", len(m.lists[3].Items()), m.myPRsSection == 1, m.draftsCollapsed)
      // ... join parts vertically
  }
  ```

**view.go — renderHeader (tab bar):**
- Update tab 1 label to show `My PRs (active:draft)` counts

**view.go — renderFooter:**
- Add `s:section | x:fold` hints to tab 1 footer (currently only on tab 0)

**view.go — renderHelpOverlay:**
- Update section switch description to mention both tabs

### 5. Tests

**store tests (`internal/store/store_test.go`):**
- Add test for upsert + query round-trip with `IsDraft: true`
- Verify draft PRs are returned by `GetPendingByRole("author")`
- Verify `is_draft` survives the migration on existing DBs

**TUI tests (`internal/tui/model_test.go`):**
- Add test that authored PRs split correctly into active vs draft sections
- Verify `activeListIndex()` returns correct index for tab 1 sections

## File Change Summary

| File | Change |
|------|--------|
| `internal/poller/types.go` | Add `IsDraft` to PollResult |
| `internal/poller/authored.go` | Remove `-is:draft`, add `IsDraft` to GraphQL, set field |
| `internal/store/store.go` | Add column migration, update PR struct, UpsertPR, GetPendingByRole |
| `internal/tui/model.go` | Add `IsDraft` to PR, 4th list, section state, routing, split logic, keys |
| `internal/tui/view.go` | Add `renderMyPRsSections`, update header/footer/help |
| `cmd/pr-monitor/main.go` | Wire `IsDraft` in both adapters |
| `internal/store/store_test.go` | Add draft round-trip tests |
| `internal/tui/model_test.go` | Add section split tests |

## Implementation Order

1. Poller + types (bottom-up: data source first)
2. Store schema + CRUD
3. Adapter wiring in main.go
4. TUI model changes (4th list, section state, routing)
5. TUI view changes (rendering, header, footer)
6. Tests
7. Build + vet + test verification

## Notes

- The `Cleanup()` function marks PRs as dismissed when they disappear from poll results. Since we're now including drafts, a PR transitioning from draft → active (or vice versa) within the same poll won't cause issues — the `pr_id` stays the same, and `UpsertPR` updates `is_draft` on conflict.
- Draft PRs have no review activity, so `LastActivity*` fields will be nil/zero. They'll sort to the bottom of the authored list before the split, which is fine since they go to their own section.
- The `-is:draft` filter is also present in the reviewer queries (`reviews.go`). We are NOT changing those — you don't review drafts. Only the authored query changes.
