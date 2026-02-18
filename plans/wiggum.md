# Ralph Wiggum Loops — pr-monitor

> **Purpose**: Framework for running iterative AI agent loops to build pr-monitor
> **Last Updated**: 2026-02-18
> **Adapted from**: stark-alarm-service/plans/wiggum.md
> **Stack**: Go, Bubble Tea, SQLite (modernc.org/sqlite), GitHub GraphQL API

---

## 1. What Is a Ralph Wiggum Loop?

A Ralph Wiggum loop is an iterative AI development technique where an AI coding
agent is given the same prompt repeatedly in a loop until a task is complete.
Each iteration starts with a fresh context window — state lives in files and
git, not in LLM memory.

```bash
# The essence
while :; do cat PROMPT.md | agent ; done
```

For background on the pattern, see `stark-alarm-service/plans/wiggum.md` §1-3.
This document focuses on the **pr-monitor-specific** task decomposition and phases.

---

## 2. Verification Commands

Every loop iteration runs this chain:

```bash
# Standard verification
go build -o /dev/null ./... && go vet ./... && go test ./...
```

### Verification Matrix by Change Type

| What Changed | Minimum Verification |
|-------------|---------------------|
| `.go` files (logic) | `go build && go vet && go test ./...` |
| `go.mod` / `go.sum` | `go mod tidy && go build ./...` |
| TUI rendering | `go build && go test ./internal/tui/...` (manual visual check) |
| Config changes | `go test ./internal/config/...` |
| SQLite schema | `go test ./internal/store/...` |

---

## 3. Task Definition Template

Tasks are defined in phase files with JSON metadata and markdown sections.

```markdown
## Task X.Y: Descriptive Title

[Detailed description]

### Context
- [Relevant files, packages, references]

### What to Do
1. [Step-by-step instructions]

### Success Criteria
1. [ ] [Specific, machine-verifiable criterion]
2. [ ] `go build -o /dev/null ./...` succeeds
3. [ ] `go vet ./...` is clean
4. [ ] `go test ./...` — all tests pass

### Constraints
- [Rules the agent must follow]

### Delegation Strategy
- **Delegate to subagents**: [exploration tasks]
- **Keep in main context**: [implementation tasks]

### Output
When all criteria are met, output: DONE
```

---

## 4. Guardrails — Seed Rules

These should be placed in `.ralph/guardrails.md` before the first loop.

```markdown
# Guardrails — pr-monitor

> Rules learned from previous iterations. READ THESE FIRST before starting work.

---

### Sign: Module path is github.com/user/pr-monitor
- **Trigger**: Creating any new .go file or import
- **Instruction**: Use the module path from go.mod for all imports
- **Added**: Seed

### Sign: Package structure follows daemon+TUI split design
- **Trigger**: Adding new functionality
- **Instruction**: Place code in the correct internal/ package:
  - `internal/poller` — GitHub API polling only
  - `internal/store` — SQLite CRUD and delta detection only
  - `internal/notify` — OSC toast and status JSON writing only
  - `internal/tui` — Bubble Tea UI only
  - `internal/discover` — filesystem repo scanning only
  - `internal/config` — YAML config loading only
  Packages communicate through interfaces and the store, NOT direct imports of each other.
- **Added**: Seed — enables future daemon+TUI binary split

### Sign: Poller and TUI must not import each other
- **Trigger**: Writing import statements in poller or tui packages
- **Instruction**: These packages communicate through `internal/store` (shared SQLite).
  The poller writes data, the TUI reads data. This separation is critical for the
  future v2 daemon+TUI split.
- **Added**: Seed — architecture constraint

### Sign: Use modernc.org/sqlite, not mattn/go-sqlite3
- **Trigger**: Adding SQLite dependency
- **Instruction**: Use `modernc.org/sqlite` (pure Go, no CGo). Do NOT use `mattn/go-sqlite3`.
- **Added**: Seed — CGo-free build requirement for simple distribution

### Sign: Auth comes from gh CLI
- **Trigger**: Needing a GitHub API token
- **Instruction**: Shell out to `gh auth token` to get the token. Do NOT hardcode tokens
  or read from environment variables for MVP.
- **Added**: Seed — decision Q7

### Sign: GitHub GraphQL, not REST
- **Trigger**: Making GitHub API calls
- **Instruction**: Use GraphQL via `github.com/shurcooL/githubv4` for all GitHub queries.
  GraphQL lets us fetch review requests + authored PRs in minimal round trips.
- **Added**: Seed — tech stack decision

### Sign: OSC escape sequences for notifications
- **Trigger**: Sending toast notifications
- **Instruction**: Use OSC 9 format: `\033]9;message\033\\` for wezterm toast.
  Write to stdout. No external notification libraries.
- **Added**: Seed — decision Q3

### Sign: Bubble Tea for TUI
- **Trigger**: Building terminal UI components
- **Instruction**: Use `github.com/charmbracelet/bubbletea` for the TUI framework
  and `github.com/charmbracelet/lipgloss` for styling. Follow the Elm architecture
  (Model, Update, View).
- **Added**: Seed — tech stack decision

### Sign: Status JSON for wezterm integration
- **Trigger**: Updating PR counts or state for external consumption
- **Instruction**: Write status to `~/.config/pr-monitor/status.json` after every
  poll cycle. Format: `{"review_count":N,"authored_activity_count":N,"last_updated":"...","oldest_review_age_hours":N}`
- **Added**: Seed — wezterm reads this file for status bar badge

### Sign: Use testify/assert for tests
- **Trigger**: Writing test files
- **Instruction**: Use `github.com/stretchr/testify/assert` for assertions.
  Table-driven tests where applicable.
- **Added**: Seed — consistency with other projects

### Sign: Config file is YAML at ~/.config/pr-monitor/config.yaml
- **Trigger**: Reading or writing configuration
- **Instruction**: Use `gopkg.in/yaml.v3` for config. Default location is
  `~/.config/pr-monitor/config.yaml`. Support XDG_CONFIG_HOME override.
- **Added**: Seed — decision Q4/Q6
```

---

## 5. Phase Decomposition

### Dependency Graph

```
Phase 0 (manual):
  Foundation (go mod init, project structure, config, CLAUDE.md)
        │
Phase 1 (can run in parallel):
  1.1 Config loader ──┐
  1.2 SQLite store ───┤
  1.3 GitHub auth ────┘
        │
Phase 2 (after 1.3):
  2.1 GitHub poller — review requests
  2.2 GitHub poller — authored PR activity
        │
Phase 3 (after 1.2 + 2.x):
  3.1 Delta detection + notification engine
  3.2 Status JSON writer
        │
Phase 4 (after 1.2 + 2.x):
  4.1 Repo discovery — filesystem scanner
        │
Phase 5 (after 3.x + 4.1):
  5.1 TUI — model + list view
  5.2 TUI — keybindings + actions (review launch, dismiss, browser)
  5.3 TUI — shame timer + styling
        │
Phase 6 (after 5.x):
  6.1 Wezterm integration (status bar Lua, auto-launch config)
  6.2 Main entrypoint — wire everything together
        │
Phase 7:
  7.1 Polish — error handling, first-run UX, graceful shutdown
  7.2 Build tooling — Makefile, install target
```

### Estimated Loops: 12-16 total

---

## 6. Phase Details

### Phase 0: Foundation (Manual — No Ralph)

Do this by hand before starting loops.

| Step | Task | Verification |
|------|------|-------------|
| 0.1 | `go mod init`, create directory structure | `go build ./...` |
| 0.2 | Create `cmd/pr-monitor/main.go` stub | `go build -o /dev/null ./...` |
| 0.3 | Create empty package files for all `internal/` packages | `go build ./...` |
| 0.4 | Seed `.ralph/guardrails.md` | File exists |
| 0.5 | Create CLAUDE.md with project context | File exists |
| 0.6 | Copy `.ralph/loop.sh` and `.ralph/run-all.sh` from stark-alarm-service | Scripts executable |

#### Directory Structure to Create

```
pr-monitor/
├── cmd/
│   └── pr-monitor/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── poller/
│   │   └── poller.go
│   ├── store/
│   │   └── store.go
│   ├── notify/
│   │   └── notify.go
│   ├── tui/
│   │   └── tui.go
│   └── discover/
│       └── discover.go
├── plans/
│   ├── pr-monitor-plan.md
│   ├── pr-monitor-final.md
│   └── wiggum.md
├── .ralph/
│   ├── guardrails.md
│   ├── loop.sh
│   ├── run-all.sh
│   ├── phase1.md
│   ├── phase2.md
│   ├── phase3.md
│   ├── phase4.md
│   ├── phase5.md
│   ├── phase6.md
│   └── phase7.md
├── go.mod
├── go.sum
└── CLAUDE.md
```

---

### Phase 1: Foundation Packages

> **Goal**: Config loading, SQLite store, and GitHub auth — the three pillars everything else depends on.

```json
[
  {
    "id": "1.1",
    "name": "YAML config loader",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/config/...",
    "max_iterations": 10
  },
  {
    "id": "1.2",
    "name": "SQLite store with schema",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/store/...",
    "max_iterations": 15
  },
  {
    "id": "1.3",
    "name": "GitHub auth via gh CLI",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/poller/...",
    "max_iterations": 10
  }
]
```

#### Task 1.1: YAML Config Loader

**What to build**: `internal/config/config.go` — loads and validates config from YAML.

**Config struct**:
```go
type Config struct {
    GitHub        GitHubConfig        `yaml:"github"`
    WorkspaceDirs []string            `yaml:"workspace_dirs"`
    RepoOverrides map[string]string   `yaml:"repo_overrides"`
    Clone         CloneConfig         `yaml:"clone"`
    ShameTimer    ShameTimerConfig    `yaml:"shame_timer"`
    Notifications NotificationConfig  `yaml:"notifications"`
}

type GitHubConfig struct {
    ReviewTeams  []string      `yaml:"review_teams"`
    Org          string        `yaml:"org"`
    PollInterval time.Duration `yaml:"poll_interval"`
}

type CloneConfig struct {
    DefaultDir      string `yaml:"default_dir"`
    PromptOnMissing bool   `yaml:"prompt_on_missing"`
}

type ShameTimerConfig struct {
    Green  int `yaml:"green"`   // hours
    Yellow int `yaml:"yellow"`
    Red    int `yaml:"red"`
}

type NotificationConfig struct {
    ToastEnabled   bool   `yaml:"toast_enabled"`
    StatusJSONPath string `yaml:"status_json_path"`
}
```

**Key behaviors**:
- `Load(path string) (*Config, error)` — load from file
- `LoadDefault() (*Config, error)` — load from `~/.config/pr-monitor/config.yaml`
- Apply defaults when fields are missing (poll_interval: 3m, shame green/yellow/red: 4/24/48)
- Validate required fields (org, at least one workspace_dir)
- Expand `~` in paths

**Tests**: Valid config, missing file (use defaults), invalid YAML, missing required fields, default values applied.

---

#### Task 1.2: SQLite Store with Schema

**What to build**: `internal/store/store.go` — SQLite database with schema creation and CRUD operations.

**Schema** (create on first open):
```sql
CREATE TABLE IF NOT EXISTS pull_requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id           TEXT NOT NULL UNIQUE,
    repo            TEXT NOT NULL,
    number          INTEGER NOT NULL,
    title           TEXT NOT NULL,
    author          TEXT NOT NULL,
    url             TEXT NOT NULL,
    role            TEXT NOT NULL CHECK(role IN ('reviewer', 'author')),
    files_changed   INTEGER DEFAULT 0,
    ci_status       TEXT DEFAULT 'unknown',
    first_seen      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notified_at     DATETIME,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'dismissed', 'reviewed')),
    last_activity_at   DATETIME,
    last_activity_type TEXT,
    last_activity_by   TEXT,
    UNIQUE(repo, number)
);

CREATE INDEX IF NOT EXISTS idx_pr_role_status ON pull_requests(role, status);
CREATE INDEX IF NOT EXISTS idx_pr_repo ON pull_requests(repo);
```

**Key types and methods**:
```go
type Store struct { db *sql.DB }

type PR struct {
    // mirrors the schema columns
}

func New(dbPath string) (*Store, error)           // open + create schema
func (s *Store) UpsertPR(ctx context.Context, pr PR) error
func (s *Store) GetPendingByRole(ctx context.Context, role string) ([]PR, error)
func (s *Store) Dismiss(ctx context.Context, prID string) error
func (s *Store) MarkReviewed(ctx context.Context, prID string) error
func (s *Store) MarkNotified(ctx context.Context, prID string) error
func (s *Store) FindNew(ctx context.Context, role string) ([]PR, error)  // notified_at IS NULL
func (s *Store) Cleanup(ctx context.Context) error  // remove merged/closed PRs not seen in last poll
func (s *Store) Close() error
```

**Tests**: Use in-memory SQLite (`:memory:`). Test upsert, query by role, dismiss, mark notified, delta detection (FindNew), cleanup.

---

#### Task 1.3: GitHub Auth via gh CLI

**What to build**: Auth resolution in `internal/poller/auth.go` — get a GitHub token from `gh auth token`.

**Key function**:
```go
func ResolveToken() (string, error)  // shell out to `gh auth token`
func ValidateScopes(token string) error  // check read:org scope via GitHub API
```

**Tests**: Test token parsing (mock exec), test error handling (gh not installed, not authed).

---

### Phase 2: GitHub Poller

> **Goal**: Fetch real PR data from GitHub using GraphQL.
> **Depends on**: Phase 1.3 (auth)

```json
[
  {
    "id": "2.1",
    "name": "Poll for review requests (personal + team)",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/poller/...",
    "max_iterations": 15
  },
  {
    "id": "2.2",
    "name": "Poll for authored PR activity",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/poller/...",
    "max_iterations": 15
  }
]
```

#### Task 2.1: Poll for Review Requests

**What to build**: `internal/poller/reviews.go` — GraphQL query for PRs where user or their teams are requested reviewers.

**GitHub GraphQL approach**: Use the search API with combined queries:
- `is:open is:pr review-requested:@me`
- `is:open is:pr team-review-requested:{org}/{team}` for each configured team

**Returns**: `[]PollResult` containing repo, PR number, title, author, files changed, CI status, URL, GitHub node ID.

**Key considerations**:
- Deduplicate results (same PR may match personal + team request)
- Parse `statusCheckRollup` for CI status
- Handle pagination (unlikely to exceed 100 but handle it)
- Rate limit awareness (log remaining rate limit from response headers)

---

#### Task 2.2: Poll for Authored PR Activity

**What to build**: `internal/poller/authored.go` — GraphQL query for open PRs authored by the user with recent review/comment activity.

**GitHub GraphQL approach**: Search `is:open is:pr author:@me`, then for each PR fetch `timelineItems` filtered to `PULL_REQUEST_REVIEW` and `ISSUE_COMMENT` types.

**Returns**: `[]PollResult` with additional fields: `LastActivityAt`, `LastActivityType` (approved/commented/changes_requested), `LastActivityBy`.

**Key considerations**:
- Only return PRs with activity newer than what's in the store
- Map GitHub review states: APPROVED → approved, CHANGES_REQUESTED → changes_requested, COMMENTED → commented

---

### Phase 3: Notifications + Delta Detection

> **Goal**: Wire poller → store → notifications pipeline.
> **Depends on**: Phase 1.2 (store) + Phase 2 (poller)

```json
[
  {
    "id": "3.1",
    "name": "Delta detection and OSC toast notifications",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/notify/...",
    "max_iterations": 15
  },
  {
    "id": "3.2",
    "name": "Status JSON writer",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/notify/...",
    "max_iterations": 10
  }
]
```

#### Task 3.1: Delta Detection + OSC Toast

**What to build**: `internal/notify/toast.go`

**Flow**: After each poll cycle, compare results against store. For PRs where `notified_at IS NULL`, emit an OSC 9 toast and mark as notified.

**Toast formats**:
- Review request: `"PR Review: {repo} #{number} — {title} (by @{author})"`
- Approved: `"PR Approved: {repo} #{number} — {title} (by @{activity_by})"`
- Commented: `"PR Comment: {repo} #{number} — {title} (by @{activity_by})"`
- Changes requested: `"Changes Requested: {repo} #{number} — {title} (by @{activity_by})"`

**Key function**: `func EmitToast(message string)` — writes `\033]9;{message}\033\\` to stdout.

---

#### Task 3.2: Status JSON Writer

**What to build**: `internal/notify/status.go`

**Key function**: `func WriteStatus(path string, reviewCount, authoredCount int, oldestAge time.Duration) error`

Writes atomic JSON update (write to temp file, rename) to avoid wezterm reading partial writes.

---

### Phase 4: Repo Discovery

> **Goal**: Scan local filesystem for repos matching GitHub remotes.
> **Depends on**: Phase 1.1 (config for workspace_dirs)

```json
[
  {
    "id": "4.1",
    "name": "Filesystem repo scanner with worktree support",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/discover/...",
    "max_iterations": 15
  }
]
```

#### Task 4.1: Repo Discovery

**What to build**: `internal/discover/discover.go`

**Key types and methods**:
```go
type Index struct {
    repos     map[string]string // "org/repo" → "/local/path"
    overrides map[string]string // from config
}

func NewIndex(workspaceDirs []string, overrides map[string]string) *Index
func (idx *Index) Scan() error                         // walk filesystem, build map
func (idx *Index) Resolve(repo string) (string, bool)  // lookup local path
```

**Scanning logic**:
- Walk each workspace dir looking for `.git` entries
- For directories with `.git/` (regular clones): parse `.git/config` for `[remote "origin"]` URL
- For directories with `.git` file (worktrees): read the file, resolve parent repo, get remote from parent
- Parse GitHub URLs: `git@github.com:org/repo.git` and `https://github.com/org/repo.git` → `org/repo`
- Config overrides take precedence over discovered paths

**Tests**: Create temp directories with mock .git/config files. Test regular clone, worktree, SSH and HTTPS URL parsing, override precedence.

---

### Phase 5: TUI

> **Goal**: Interactive terminal UI with Bubble Tea.
> **Depends on**: Phase 3 (notifications — for data flow) + Phase 4 (discover — for review launch)

```json
[
  {
    "id": "5.1",
    "name": "TUI model + list view",
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
    "name": "TUI shame timer + styling",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/tui/...",
    "max_iterations": 10
  }
]
```

#### Task 5.1: TUI Model + List View

**What to build**: `internal/tui/model.go`, `internal/tui/view.go`

**Bubble Tea model**:
```go
type Model struct {
    store      *store.Store
    discover   *discover.Index
    tabs       []string          // ["To Review", "My PRs"]
    activeTab  int
    reviewList list.Model        // bubbletea list component
    authorList list.Model
    width      int
    height     int
}
```

**Two tabs**: "To Review" showing PRs where role=reviewer, "My PRs" showing role=author.
Each row displays: repo | #number | title | CI/status | age.

---

#### Task 5.2: TUI Keybindings + Actions

**Keybindings to implement**:
- `Tab` / `Shift+Tab` — switch tabs
- `j`/`k` or arrows — navigate
- `r` or `Enter` — launch review (To Review tab)
- `Enter` — jump to repo (My PRs tab)
- `d` — dismiss
- `o` — open in browser (`open {url}` on macOS)
- `R` — force refresh
- `?` — help overlay
- `q` — quit

**Review launch action**:
1. `discover.Resolve(repo)` to get local path
2. If not found: show prompt (clone/browser/dismiss) — can be a simple confirmation in the TUI
3. If found: exec `wezterm cli split-pane --cwd {path} -- claude -p "/review-pr {number}"`

---

#### Task 5.3: TUI Shame Timer + Styling

**Color the age column based on config thresholds**:
- Green: < `shame_timer.green` hours (default 4h)
- Yellow: `green` to `yellow` hours (4-24h)
- Orange: `yellow` to `red` hours (24-48h)
- Red: > `red` hours (48h+)

Use Lip Gloss styles for coloring. Apply to the age column in both list views.

---

### Phase 6: Wezterm + Main Entrypoint

> **Goal**: Wire everything together and provide wezterm integration.
> **Depends on**: Phase 5 (TUI)

```json
[
  {
    "id": "6.1",
    "name": "Wezterm integration snippets",
    "test_command": "test -f wezterm/pr-monitor.lua",
    "max_iterations": 5
  },
  {
    "id": "6.2",
    "name": "Main entrypoint — wire all packages",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./...",
    "max_iterations": 15
  }
]
```

#### Task 6.1: Wezterm Lua Snippets

**What to create**: `wezterm/pr-monitor.lua` — ready-to-include snippet for user's wezterm.lua.

Contains:
- `update-right-status` handler reading status.json for badge
- `gui-startup` handler spawning pr-monitor in a dedicated tab

---

#### Task 6.2: Main Entrypoint

**What to build**: `cmd/pr-monitor/main.go`

Wires together:
1. Load config
2. Resolve GitHub token
3. Open SQLite store
4. Build repo discovery index (scan in background)
5. Start poller in a goroutine (poll → upsert store → detect deltas → toast → write status JSON)
6. Start Bubble Tea TUI (blocks on main goroutine)
7. Graceful shutdown on SIGINT/SIGTERM (cancel context, close store)

---

### Phase 7: Polish

> **Goal**: Error handling, first-run experience, build tooling.

```json
[
  {
    "id": "7.1",
    "name": "Error handling and first-run UX",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./...",
    "max_iterations": 10
  },
  {
    "id": "7.2",
    "name": "Makefile and install target",
    "test_command": "make build && make test",
    "max_iterations": 5
  }
]
```

#### Task 7.1: Error Handling + First-Run

- GitHub rate limit detection and backoff
- Network failure retry with exponential backoff
- Expired auth detection with helpful error message
- First run: create `~/.config/pr-monitor/` dir, generate default config.yaml, validate auth
- `--version` and `--help` flags

#### Task 7.2: Makefile

```makefile
build:    go build -o bin/pr-monitor ./cmd/pr-monitor
test:     go test ./...
install:  go install ./cmd/pr-monitor
lint:     go vet ./...
clean:    rm -rf bin/
```

---

## 7. Subagent Strategy per Phase

| Phase | Delegate to Subagents | Keep in Main Context |
|-------|----------------------|----------------------|
| 1.1 Config | Read gopkg.in/yaml.v3 docs for custom unmarshal patterns | Write config.go + tests |
| 1.2 Store | Read modernc.org/sqlite docs for driver setup | Write store.go + tests |
| 1.3 Auth | Read `gh auth` help output, explore token file format | Write auth.go + tests |
| 2.x Poller | Read githubv4 docs, explore GraphQL schema for PR search | Write query functions + tests |
| 3.x Notify | Read OSC escape sequence specs | Write toast.go, status.go + tests |
| 4.1 Discover | Read git worktree internals (`.git` file format) | Write discover.go + tests |
| 5.x TUI | Read Bubble Tea examples, list component docs | Write model/view/update + tests |
| 6.x Wire | Read all internal/ package interfaces | Write main.go, wezterm Lua |

---

## 8. CLAUDE.md Content

Add this to the project CLAUDE.md before starting loops:

```markdown
## Ralph Loop Context

This project is being developed using Ralph Wiggum Loops.
- Task definitions: .ralph/phase*.md
- Guardrails: .ralph/guardrails.md (READ FIRST)
- Design reference: plans/pr-monitor-final.md
- Loop runner: plans/wiggum.md

## What This Tool Does

pr-monitor is a terminal-native GitHub PR monitoring tool. It:
1. Polls GitHub for PRs requiring user's review (personal + team-based)
2. Polls GitHub for activity on PRs the user authored
3. Persists state in SQLite, detects new events via delta comparison
4. Renders a Bubble Tea TUI with two tabs: "To Review" and "My PRs"
5. Fires OSC toast notifications for new events
6. Writes status JSON for wezterm status bar badge
7. Launches review workflow: wezterm pane → repo → claude-code → /review-pr

## Package Map

- `cmd/pr-monitor/` — entrypoint, wires everything
- `internal/config/` — YAML config loading
- `internal/poller/` — GitHub GraphQL polling
- `internal/store/` — SQLite persistence + delta detection
- `internal/notify/` — OSC toast + status JSON
- `internal/tui/` — Bubble Tea interactive UI
- `internal/discover/` — local repo filesystem scanner

## Architecture Rule

Poller and TUI do NOT import each other. They communicate through Store.
This enables a future split into separate daemon + TUI binaries.

## Verification

Always run before committing:
go build -o /dev/null ./... && go vet ./... && go test ./...
```

---

## 9. Running the Loops

### Interactive Mode (Recommended for Phase 1)

```
Manual Loop Protocol:
1. Open Claude Code in the pr-monitor directory
2. Paste a task section from the phase file as your prompt
3. Let the agent work toward success criteria
4. When it completes or gets stuck:
   a. Run verification: go build && go vet && go test ./...
   b. If PASS → commit and move to next task
   c. If FAIL → note what went wrong in .ralph/guardrails.md
   d. Commit progress: git add -A && git commit -m "ralph: iteration N"
   e. Start a NEW Claude Code session (fresh context)
   f. Paste the same prompt again
```

### Scripted Mode (After Phase 1)

```bash
# Run a single phase
.ralph/loop.sh .ralph/phase2.md

# Run a specific task
.ralph/loop.sh .ralph/phase3.md 3.1

# Run all phases from phase 2 onward
.ralph/run-all.sh 2
```

### Concurrency

Start with 1 loop at a time. Move to parallel worktrees after Phase 2 is solid:
- Phases 3 and 4 can run in parallel (no dependencies on each other)
- Phase 5 tasks (5.1, 5.2, 5.3) are sequential

---

## 10. Cost & Practical Considerations

| Factor | Detail |
|--------|--------|
| **Estimated loops** | 12-16 total across all phases |
| **Time per loop** | 3-8 min (Go compiles fast, good error messages) |
| **Total estimated time** | 1-2 hours of loop execution |
| **Token cost** | Each iteration consumes a context window. Use with flat-rate plan. |
| **Human oversight** | Check progress between iterations. Review guardrails after failures. |
| **Go advantage** | Compiler catches type errors immediately — fewer wasted iterations. |
