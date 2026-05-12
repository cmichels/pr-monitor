# Plan: Add Jira Tab to pr-monitor

## Context

pr-monitor currently has 3 tabs: To Review, My PRs, Stats. We're adding a 4th "Jira" tab that shows issues from 2 saved Jira filters plus any assigned-to-me To Do/In Progress issues not covered by those filters. The tab follows the existing split-panel pattern (list left, detail right) and launches `claude '/worktree <issue_key>'` in a wezterm tab on selection.

**Data sources:**
- Filter 13066 ("Submissions"): child tasks under OP-1735, unassigned or assigned to me, status To Do
- Filter 12562 ("Knowledge"): team queue across ~8 reporters/assignees, statuses To Do/In Progress/Blocked/In Review
- "My Tasks": `assignee = currentUser() AND status IN ("To Do", "In Progress")` minus issues already in the two filters

**Auth:** Shell out to `acli` CLI (already authenticated on machine at `/opt/homebrew/bin/acli`)

---

## Package Map (new files marked with +)

```
internal/config/config.go          — add JiraConfig struct
+ internal/jira/jira.go            — acli wrapper: search by filter, search by JQL, view issue detail
+ internal/jira/types.go           — JiraIssue, JiraComment, JiraDetail types (JSON parse targets)
+ internal/store/jira.go           — SQLite table + UpsertJiraIssue, GetJiraIssues, GetJiraIssueByKey
+ internal/tui/jira_item.go        — JiraItem (implements list.DefaultItem), JiraLoader interface
+ internal/tui/jira_view.go        — renderJiraTab (3 stacked sections), renderJiraDetail
+ internal/tui/jira_cmd.go         — launchWorktree cmd, loadJiraData cmd, fetchJiraDetail cmd
  internal/tui/model.go            — add Jira state fields, tab index 3, JiraLoader option
  internal/tui/view.go             — route tab 3 to renderJiraTab, update header/footer
  internal/tui/keys.go             — Jira tab keybindings (same as PR tabs + enter launches worktree)
  cmd/pr-monitor/main.go           — jiraAdapter, jiraLoop goroutine, WithJiraLoader option
```

---

## Step 1: Config — `internal/config/config.go`

Add `JiraConfig` to `Config`:

```go
type JiraConfig struct {
    BaseURL      string       `yaml:"base_url"`   // "https://example.atlassian.net"
    Filters      []JiraFilter `yaml:"filters"`
    MyTasksJQL   string       `yaml:"my_tasks_jql"` // fallback JQL for "assigned to me" query
    PollInterval time.Duration `yaml:"poll_interval"`
}

type JiraFilter struct {
    ID   int    `yaml:"id"`
    Name string `yaml:"name"`
}
```

- Default `poll_interval`: 5m
- Default `my_tasks_jql`: `project = OP AND assignee = currentUser() AND status IN ("To Do", "In Progress") ORDER BY updated DESC`
- Custom `UnmarshalYAML` for duration (reuse pattern from `GitHubConfig`)
- Add to `DefaultConfig()`, `applyDefaults()`, `expandPaths()` (no paths to expand), `validate()` (warn if no filters configured)
- Update `defaultConfigYAML` template in `main.go`

---

## Step 2: Jira Client — `internal/jira/`

### `types.go`
```go
type Issue struct {
    Key        string
    Summary    string
    Status     string
    StatusCat  string   // "new", "indeterminate", "done"
    Priority   string
    IssueType  string
    Assignee   string
    Reporter   string
    Labels     []string
    SelfURL    string   // REST API self URL (for building browse URL)
}

type IssueDetail struct {
    Issue
    Description string   // ADF content flattened to plain text
    Comments    []Comment
}

type Comment struct {
    Author    string
    Body      string  // ADF flattened
    CreatedAt string
}
```

### `jira.go`
Wrapper around `acli` CLI. All methods shell out and parse JSON:

- `SearchByFilter(filterID int) ([]Issue, error)` — `acli jira workitem search --filter <id> --json --paginate --fields "key,summary,status,priority,assignee,reporter,labels,issuetype"`
- `SearchByJQL(jql string) ([]Issue, error)` — `acli jira workitem search --jql <jql> --json --paginate --fields "key,summary,status,priority,assignee,reporter,labels,issuetype"`
- `GetIssueDetail(key string) (*IssueDetail, error)` — `acli jira workitem view <key> --json --fields "key,summary,status,priority,assignee,reporter,description,labels,comment"`
- Helper: `flattenADF(node) string` — recursively extracts text from Jira ADF (Atlassian Document Format) JSON. Only needs to handle `paragraph`, `text`, `heading`, `bulletList`, `listItem`, `codeBlock` node types.
- Helper: `parseIssueJSON(data) ([]Issue, error)` — parses acli JSON array output into `[]Issue`
- Helper: `BrowseURL(baseURL, key string) string` — returns `baseURL + "/browse/" + key`

---

## Step 3: Store — `internal/store/jira.go`

### Schema
```sql
CREATE TABLE IF NOT EXISTS jira_issues (
    issue_key   TEXT PRIMARY KEY,
    summary     TEXT NOT NULL,
    status      TEXT NOT NULL,
    status_cat  TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT '',
    issue_type  TEXT NOT NULL DEFAULT '',
    assignee    TEXT NOT NULL DEFAULT '',
    reporter    TEXT NOT NULL DEFAULT '',
    labels      TEXT NOT NULL DEFAULT '',  -- JSON array
    source      TEXT NOT NULL DEFAULT '',  -- "filter:13066", "filter:12562", "my_tasks"
    browse_url  TEXT NOT NULL DEFAULT '',
    first_seen  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Methods
- `UpsertJiraIssue(ctx, issue JiraIssue) error` — INSERT OR REPLACE
- `GetJiraIssuesBySource(ctx, source string) ([]JiraIssue, error)` — for populating sections
- `GetAllJiraIssues(ctx) ([]JiraIssue, error)` — all issues
- `CleanupJiraIssues(ctx, currentKeys []string) error` — remove issues no longer in results
- `GetJiraIssueCounts(ctx) (map[string]int, error)` — count by source for tab header

### JiraIssue struct (store-level)
```go
type JiraIssue struct {
    Key       string
    Summary   string
    Status    string
    StatusCat string
    Priority  string
    IssueType string
    Assignee  string
    Reporter  string
    Labels    string  // JSON array string
    Source    string
    BrowseURL string
    FirstSeen time.Time
    UpdatedAt time.Time
}
```

---

## Step 4: TUI Types — `internal/tui/jira_item.go`

### JiraItem (list.DefaultItem implementation)
```go
type JiraItem struct {
    issue JiraIssue
}

type JiraIssue struct { /* TUI-level mirror of store.JiraIssue */ }

func (i JiraItem) Title() string       // "OP-3174  Implement Show All..."
func (i JiraItem) Description() string  // "Task | Medium | To Do | Stephen Yager"
func (i JiraItem) FilterValue() string  // key + summary for filtering
```

### JiraLoader interface
```go
type JiraLoader interface {
    GetJiraIssuesBySource(ctx context.Context, source string) ([]JiraIssue, error)
}
```

### JiraDetailFetcher interface
```go
type JiraDetailFetcher interface {
    GetIssueDetail(ctx context.Context, key string) (*JiraDetail, error)
}
```

### JiraDetail struct
```go
type JiraDetail struct {
    Key         string
    Summary     string
    Status      string
    Priority    string
    IssueType   string
    Assignee    string
    Reporter    string
    Labels      []string
    Description string
    Comments    []JiraComment
}
type JiraComment struct {
    Author    string
    Body      string
    CreatedAt string
}
```

---

## Step 5: TUI Model Changes — `internal/tui/model.go`

### New fields on Model
```go
// Jira tab state (tab 3)
jiraLoader        JiraLoader
jiraDetailFetcher JiraDetailFetcher
jiraSection       int   // 0=submissions, 1=knowledge, 2=my_tasks
jiraCollapsed     [3]bool
jiraDetail        *JiraDetail
jiraDetailKey     string
jiraDetailLoading bool
jiraDetailErr     error
jiraDetailCache   map[string]*JiraDetail
jiraBaseURL       string  // for building browse URLs
```

### New list.Models
Add 3 more lists to `m.lists` (indices 4, 5, 6):
- `lists[4]` = Submissions (filter 13066)
- `lists[5]` = Knowledge (filter 12562)
- `lists[6]` = My Tasks

### Tab registration
```go
tabs := []string{"To Review", "My PRs", "Stats", "Jira"}
```

### activeListIndex() update
```go
case 3:
    return 4 + m.jiraSection
```

### New Option functions
```go
func WithJiraLoader(l JiraLoader) Option
func WithJiraDetailFetcher(f JiraDetailFetcher) Option
func WithJiraBaseURL(url string) Option
```

### New messages
```go
type jiraDataLoadedMsg struct {
    submissions []JiraItem
    knowledge   []JiraItem
    myTasks     []JiraItem
    err         error
}
type jiraDetailLoadedMsg struct {
    key    string
    detail *JiraDetail
    err    error
}
type JiraRefreshMsg struct{}
```

---

## Step 6: TUI View — `internal/tui/jira_view.go`

### renderJiraTab(m Model) string
Three stacked sections following `renderStackedSections` pattern:
- "Submissions" section (lists[4])
- "Knowledge" section (lists[5])
- "My Tasks" section (lists[6])

Each with collapse toggle and section count.

### renderJiraDetail(m Model) string
Right panel content (set into `m.detailViewport`):
```
OP-3174  Task
─────────────────
Status:    To Do
Priority:  Medium
Assignee:  (unassigned)
Reporter:  Stephen Yager
Labels:    frontend, ux

Description
───────────
Currently, when widgets are hidden...

Comments (0)
────────────
(no comments)
```

### resizeJiraSections()
Follows `resizeStackedLists` pattern but for 3 sections. Available height split:
- If all 3 expanded: each gets ~1/3
- If one collapsed: remaining two split evenly
- If two collapsed: remaining one gets all space

---

## Step 7: TUI Commands — `internal/tui/jira_cmd.go`

### loadJiraData() tea.Cmd
Calls `jiraLoader.GetJiraIssuesBySource()` for each of the 3 sources, returns `jiraDataLoadedMsg`.

### fetchJiraDetail(fetcher, key) tea.Cmd
Calls `jiraDetailFetcher.GetIssueDetail()`, returns `jiraDetailLoadedMsg`.

### launchWorktree(item JiraItem, baseURL string) tea.Cmd
```go
func launchWorktree(item JiraItem, baseURL string) tea.Cmd {
    return func() tea.Msg {
        issueURL := baseURL + "/browse/" + item.issue.Key
        // 1. wezterm cli spawn (no --cwd needed, /worktree handles it)
        // 2. wezterm cli set-tab-title --pane-id <id> "<key>"
        // 3. time.Sleep(1500ms)
        // 4. wezterm cli send-text "claude '/worktree <issue_key>'"
        return statusMsg{text: fmt.Sprintf("Working on %s", item.issue.Key)}
    }
}
```

---

## Step 8: TUI Key Handling — `internal/tui/model.go` Update block

### In Update(), add before stats tab block:
```go
// Jira tab keybindings (tab 3)
if m.activeTab == 3 {
    // j/k navigation across 3 sections
    // s: cycle section (0→1→2→0)
    // x: collapse/expand current section
    // r/enter: launch worktree
    // o: open in browser
    // y: copy URL
    // l/h: focus detail panel / back to list
}
```

### Section switching for 3 sections
`s` key cycles: `m.jiraSection = (m.jiraSection + 1) % 3`

### Cross-boundary navigation
j at bottom of section 0 → section 1; j at bottom of section 1 → section 2
k at top of section 2 → section 1; k at top of section 1 → section 0

---

## Step 9: Update view.go

### renderView body
```go
case 3:
    listView = renderJiraTab(m)
    // Uses same detail panel split as tabs 0/1
```

### renderHeader — add case 3
```go
case 3:
    total := len(m.lists[4].Items()) + len(m.lists[5].Items()) + len(m.lists[6].Items())
    label = fmt.Sprintf("%s (%d)", tab, total)
```

### renderFooter — add case 3
```go
case m.activeTab == 3:
    legend = "tab:switch | r:worktree | o:open | y:copy url | R:refresh | s:section | x:fold | ?:help | q:quit"
```

### renderHelpOverlay — add Jira section

---

## Step 10: Wire in main.go

### jiraAdapter
```go
type jiraAdapter struct {
    store *store.Store
}
func (a *jiraAdapter) GetJiraIssuesBySource(ctx, source string) ([]tui.JiraIssue, error)
```

### jiraDetailAdapter
```go
type jiraDetailAdapter struct {
    client *jira.Client
}
func (a *jiraDetailAdapter) GetIssueDetail(ctx, key string) (*tui.JiraDetail, error)
```

### jiraLoop goroutine
```go
func jiraLoop(ctx context.Context, client *jira.Client, s *store.Store, cfg *config.JiraConfig, program *tea.Program) {
    ticker := time.NewTicker(cfg.PollInterval)
    defer ticker.Stop()
    jiraPoll(ctx, client, s, cfg) // immediate first poll
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: jiraPoll(ctx, client, s, cfg)
        }
    }
}
```

### jiraPoll
1. For each filter: `client.SearchByFilter(id)` → `store.UpsertJiraIssue` with source="filter:<id>"
2. Collect all filter issue keys into a set
3. `client.SearchByJQL(cfg.MyTasksJQL)` → for issues NOT in the filter key set, `store.UpsertJiraIssue` with source="my_tasks"
4. `store.CleanupJiraIssues(allCurrentKeys)`
5. `program.Send(tui.JiraRefreshMsg{})`

### Model creation
```go
model := tui.New(adapter, idx, shame,
    tui.WithDismisser(st),
    tui.WithDetailFetcher(detailFetcher),
    tui.WithStatsLoader(sAdapter),
    tui.WithJiraLoader(jiraAdapt),
    tui.WithJiraDetailFetcher(jiraDetailAdapt),
    tui.WithJiraBaseURL(cfg.Jira.BaseURL),
)
```

---

## Step 11: Detail Panel Reuse

The existing `detailViewport` is shared across all tabs. For the Jira tab:
- `maybeLoadJiraDetail()` checks if the selected JiraItem changed, loads from cache or fetches
- `updateDetailViewport()` checks `m.activeTab == 3` and calls `renderJiraDetail(m)` instead of `renderDetailContent(m)`
- This reuses the same viewport and focus mechanics (l/h keys, scroll, etc.)

---

## Implementation Order

1. **Config** — add JiraConfig struct
2. **Jira client** — `internal/jira/` package with acli wrapper + JSON parsing
3. **Store** — `internal/store/jira.go` with schema + CRUD
4. **TUI types** — `internal/tui/jira_item.go` with JiraItem, interfaces
5. **TUI view** — `internal/tui/jira_view.go` with section rendering + detail rendering
6. **TUI commands** — `internal/tui/jira_cmd.go` with data loading + worktree launch
7. **TUI model integration** — extend model.go, view.go, keys.go for tab 3
8. **Main wiring** — adapters, jiraLoop, options

---

## Verification

```bash
# Build check
go build -o /dev/null ./... && go vet ./...

# Unit tests
go test ./internal/jira/...   # JSON parsing, ADF flattening
go test ./internal/store/...  # SQLite CRUD for jira_issues table

# Manual test
go run ./cmd/pr-monitor/  # Verify:
# 1. Tab bar shows "Jira" as 4th tab
# 2. Tab to Jira tab, 3 sections load with real data
# 3. j/k navigates, s switches sections, x collapses
# 4. Detail panel shows issue info when highlighted
# 5. Enter/r opens wezterm tab with claude /worktree command
# 6. o opens issue in browser, y copies URL
# 7. Data refreshes on poll interval
```
