# Ralph Phase 5: TUI

> **Goal**: Interactive terminal UI with Bubble Tea — list views, keybindings, shame timer.
> **Depends on**: Phase 3 (notify — data pipeline) + Phase 4 (discover — repo resolution)
> **Design Reference**: `plans/pr-monitor-final.md` "TUI Design" section

```json
[
  {
    "id": "5.1",
    "name": "TUI model and list view",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/tui/...",
    "max_iterations": 15
  },
  {
    "id": "5.2",
    "name": "TUI keybindings and actions",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/tui/...",
    "max_iterations": 15
  },
  {
    "id": "5.3",
    "name": "TUI shame timer and styling",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/tui/...",
    "max_iterations": 10
  }
]
```

---

## Task 5.1: TUI Model and List View

Implement the core Bubble Tea application model with two tabbed list views:
"To Review" and "My PRs".

### Context

- Target file: `internal/tui/tui.go` (exists, currently just `package tui`)
- Additional files: `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/item.go`
- Store provides: `GetPendingByRole(ctx, "reviewer")` and `GetPendingByRole(ctx, "author")`
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define the list item type in `internal/tui/item.go`:

```go
// PRItem implements the bubbletea list.Item interface for displaying PRs.
type PRItem struct {
    PRID             string
    Repo             string
    Number           int
    Title            string
    Author           string
    URL              string
    FilesChanged     int
    CIStatus         string
    FirstSeen        time.Time
    Status           string
    LastActivityType string
    LastActivityBy   string
}

func (i PRItem) FilterValue() string { return i.Title }
func (i PRItem) Title() string       // format: "repo #number  title"
func (i PRItem) Description() string // format: "by @author | 3 files | CI ✓ | 2h ago"
```

2. Define the model in `internal/tui/model.go`:

```go
type Model struct {
    // Data sources (interfaces for testability)
    prLoader  PRLoader
    repoResolver RepoResolver

    // UI state
    tabs      []string
    activeTab int
    lists     []list.Model   // one per tab
    width     int
    height    int
    help      help.Model
    showHelp  bool
    err       error
}

// PRLoader abstracts the store for loading PRs.
type PRLoader interface {
    GetPendingByRole(ctx context.Context, role string) ([]PR, error)
}

// RepoResolver abstracts discover for resolving local paths.
type RepoResolver interface {
    Resolve(repo string) (string, bool)
}

// PR is the TUI's view of a pull request (maps from store.PR).
type PR struct {
    PRID             string
    Repo             string
    Number           int
    Title            string
    Author           string
    URL              string
    FilesChanged     int
    CIStatus         string
    FirstSeen        time.Time
    Status           string
    LastActivityType string
    LastActivityBy   string
}
```

3. Implement the Bubble Tea lifecycle in `internal/tui/model.go`:

```go
func New(loader PRLoader, resolver RepoResolver) Model
func (m Model) Init() tea.Cmd           // return a cmd to load initial data
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() string
```

4. Implement the view in `internal/tui/view.go`:

- Header: `"PR Monitor"` with tab bar showing `[To Review (N)]  [My PRs (N)]`
- Active tab is highlighted
- Body: the active tab's list
- Footer: keybinding hints

5. Tab layout:
- Tab 0: "To Review" — PRs where role = reviewer, sorted by age (oldest first)
- Tab 1: "My PRs" — PRs where role = author, sorted by most recent activity

6. Data loading and refresh — define custom message types:

```go
// prsLoadedMsg is returned by the data loading Cmd.
// Used both on Init and when the poll goroutine triggers a refresh.
type prsLoadedMsg struct {
    reviewPRs   []PRItem
    authoredPRs []PRItem
    err         error
}

// RefreshMsg is sent by the poll goroutine (via program.Send) to tell the
// TUI that new data is available in the store. This is an EXPORTED type
// so main.go can construct and send it.
type RefreshMsg struct{}
```

Implement a `loadData` function that returns a `tea.Cmd`:
```go
func (m Model) loadData() tea.Cmd {
    return func() tea.Msg {
        // query PRLoader for both roles, return prsLoadedMsg
    }
}
```

In `Update`, handle both message types:
- `prsLoadedMsg` → update the list items from the loaded data
- `RefreshMsg` → return `m.loadData()` cmd (triggers a re-query of the store)

**Refresh flow**: The poll goroutine in main.go calls `program.Send(tui.RefreshMsg{})` after
each poll cycle. The TUI's Update receives it, fires `loadData()`, which queries the store
and returns `prsLoadedMsg`, which updates the lists. This is the standard Bubble Tea pattern
for external event sources.

7. Write tests in `internal/tui/model_test.go`:
- Test New creates model with correct initial state
- Test tab switching (activeTab changes)
- Test data loading populates lists (prsLoadedMsg)
- Test RefreshMsg triggers data reload
- Test list item formatting (Title, Description)

### Success Criteria

1. [ ] `internal/tui/item.go` implements PRItem with list.Item interface
2. [ ] `internal/tui/model.go` implements Model with Init, Update, View
3. [ ] `internal/tui/view.go` renders header with tabs, list, and footer
4. [ ] Two tabs: "To Review" and "My PRs"
5. [ ] Data loads from PRLoader interface (not direct store import)
6. [ ] `RefreshMsg` is exported and handled in Update (triggers data reload)
7. [ ] `prsLoadedMsg` handled in Update (populates list items)
6. [ ] `internal/tui/model_test.go` tests model init, tab switching, data loading
7. [ ] `go build -o /dev/null ./...` succeeds
8. [ ] `go vet ./...` is clean
9. [ ] `go test ./internal/tui/...` — all tests pass

### Constraints

- Use `github.com/charmbracelet/bubbletea` and `github.com/charmbracelet/bubbles/list`
- Use `github.com/charmbracelet/lipgloss` for styling
- Do NOT import `internal/store` or `internal/discover` directly — use interfaces (PRLoader, RepoResolver)
- This enables the future daemon+TUI split
- Sort "To Review" by `FirstSeen` ascending (oldest first)
- Sort "My PRs" by most recent activity descending

### Delegation Strategy

- **Delegate to subagents**: Read Bubble Tea docs for list component, tab switching patterns, and Elm architecture
- **Keep in main context**: Write model.go, view.go, item.go, and tests

### Output

When all criteria are met, output: DONE

---

## Task 5.2: TUI Keybindings and Actions

Add keybindings for navigation, review launch, dismiss, browser open,
force refresh, and help overlay.

### Context

- Target files: `internal/tui/keys.go` (new), update `internal/tui/model.go`
- Model from Task 5.1 already handles tab switching
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define keybindings in `internal/tui/keys.go`:

```go
type keyMap struct {
    SwitchTab   key.Binding
    Review      key.Binding
    Dismiss     key.Binding
    OpenBrowser key.Binding
    Refresh     key.Binding
    Help        key.Binding
    Quit        key.Binding
}

func defaultKeyMap() keyMap
```

Bindings:
- `tab`/`shift+tab` — switch tabs
- `r`, `enter` — launch review (To Review tab) / jump to repo (My PRs tab)
- `d` — dismiss PR
- `o` — open in browser
- `R` (shift+r) — force refresh
- `?` — toggle help
- `q` — quit

2. Implement review launch action:

```go
// launchReview opens a new wezterm pane in the repo directory with claude-code.
func (m *Model) launchReview(pr PRItem) tea.Cmd
```

Logic:
- Call `m.repoResolver.Resolve(pr.Repo)` to get local path
- If not found: return a message indicating repo not found (TUI shows inline prompt)
- If found: exec `wezterm cli split-pane --cwd {path} -- claude -p "/review-pr {number}"`
- Use `os/exec.Command` — fire and forget (don't block the TUI)
- Note: `claude -p` runs in print mode (non-interactive). The review output will display in the pane. Wezterm keeps panes open after process exit by default, so the user can read the output. For interactive follow-up, the user can start a new claude session in the same pane.

3. Implement dismiss action:

```go
type dismissMsg struct{ prID string }

func (m *Model) dismissPR(pr PRItem) tea.Cmd
```

- Returns a `tea.Cmd` that calls the dismiss callback
- The model needs a `Dismisser` interface:
```go
type Dismisser interface {
    Dismiss(ctx context.Context, prID string) error
}
```

4. Implement browser open:

```go
func openBrowser(url string) tea.Cmd
```

- Use `exec.Command("open", url)` on macOS

5. Implement force refresh:
- Return a `tea.Cmd` that triggers data reload (same as Init)

6. Help overlay:
- Toggle with `?`
- Show all keybindings in a styled overlay
- Any key dismisses the help overlay

7. Add a status line at the bottom showing feedback messages:
- "Dismissed PR #42" (after dismiss)
- "Opened in browser" (after open)
- "Launching review for repo #42..." (after review launch)
- "Repo not found locally: org/repo" (when resolve fails)
- Messages auto-clear after 3 seconds

8. Write tests in `internal/tui/keys_test.go`:
- Test keyMap has all expected bindings
- Test keybinding help text is set
- Test Update handles each key correctly (mock interfaces)

### Success Criteria

1. [ ] `internal/tui/keys.go` defines keyMap with all keybindings
2. [ ] Review launch calls wezterm CLI with correct args
3. [ ] Dismiss removes PR from list via Dismisser interface
4. [ ] Open browser uses `open` command on macOS
5. [ ] Force refresh reloads data
6. [ ] Help overlay toggles with `?`
7. [ ] Status line shows feedback messages
8. [ ] `internal/tui/keys_test.go` tests keybinding handling
9. [ ] `go build -o /dev/null ./...` succeeds
10. [ ] `go vet ./...` is clean
11. [ ] `go test ./internal/tui/...` — all tests pass

### Constraints

- Use `github.com/charmbracelet/bubbles/key` for keybinding definitions
- Review launch command: `wezterm cli split-pane --cwd {path} -- claude -p "/review-pr {number}"`
- Browser open command: `open {url}` (macOS-specific is fine for MVP)
- Do NOT import internal/store or internal/discover directly — use interfaces
- Fire-and-forget for external commands (don't block the TUI event loop)

### Delegation Strategy

- **Delegate to subagents**: Read Bubble Tea docs for key handling, cmd patterns, exec integration
- **Keep in main context**: Write keys.go, update model.go, and tests

### Output

When all criteria are met, output: DONE

---

## Task 5.3: TUI Shame Timer and Styling

Add color-coded age display based on configurable shame timer thresholds.
Apply consistent Lip Gloss styling to the entire TUI.

### Context

- Target files: `internal/tui/style.go` (new), update `internal/tui/item.go` and `internal/tui/view.go`
- Shame timer config: green < 4h, yellow 4-24h, orange 24-48h, red > 48h
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define styles in `internal/tui/style.go`:

```go
type ShameConfig struct {
    GreenHours  int
    YellowHours int
    RedHours    int
}

var (
    greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4caf50"))
    yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9a825"))
    orangeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9800"))
    redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f44336"))
)

// AgeStyle returns the appropriate style for a PR's age.
func AgeStyle(age time.Duration, cfg ShameConfig) lipgloss.Style

// FormatAge returns a human-readable age string like "2h", "1d", "3d".
func FormatAge(age time.Duration) string
```

2. Age formatting:
- < 1 hour: `"<1h"`
- 1-23 hours: `"{n}h"`
- 1-6 days: `"{n}d"`
- 7+ days: `"{n}w"`

3. Apply shame colors to the age column in PRItem.Description():
- Green: `age < greenHours`
- Yellow: `greenHours <= age < yellowHours`
- Orange: `yellowHours <= age < redHours`
- Red: `age >= redHours`

4. Apply consistent styling to the overall TUI:
- Tab bar: active tab bold/underlined, inactive tab dimmed
- Header: styled with a border
- CI status indicators: `✓` green for passing, `✗` red for failing, `●` yellow for pending, `?` gray for unknown
- Activity type indicators for My PRs tab: `✅` approved, `💬` commented, `⚠️` changes_requested
- Footer keybinding hints: dimmed style

5. Add the ShameConfig to the Model (passed in on construction):

```go
func New(loader PRLoader, resolver RepoResolver, shame ShameConfig) Model
```

6. Write tests in `internal/tui/style_test.go`:
- Test AgeStyle returns correct style for each threshold
- Test FormatAge for various durations
- Test edge cases: exactly on threshold boundaries

### Success Criteria

1. [ ] `internal/tui/style.go` implements AgeStyle, FormatAge, and style constants
2. [ ] Age column colored based on shame timer thresholds
3. [ ] FormatAge produces human-readable strings (<1h, 2h, 1d, 3d, 2w)
4. [ ] CI status indicators rendered with colors
5. [ ] Tab bar styled with active/inactive distinction
6. [ ] `internal/tui/style_test.go` tests age styling and formatting
7. [ ] `go build -o /dev/null ./...` succeeds
8. [ ] `go vet ./...` is clean
9. [ ] `go test ./internal/tui/...` — all tests pass

### Constraints

- Use `github.com/charmbracelet/lipgloss` for all styling
- Do NOT use emoji in the shame timer — use colored text only
- CI status indicators can use unicode characters (✓, ✗, ●)
- Activity type text is fine (no need for emoji — use "approved", "commented", etc. with color)
- Color values: green=#4caf50, yellow=#f9a825, orange=#ff9800, red=#f44336

### Delegation Strategy

- **Delegate to subagents**: Read Lip Gloss docs for style composition, color handling
- **Keep in main context**: Write style.go, update item.go and view.go

### Output

When all criteria are met, output: DONE
