# Guardrails — pr-monitor

> Rules learned from previous iterations. READ THESE FIRST before starting work.

---

### Sign: Module path matches go.mod
- **Trigger**: Creating any new .go file or import
- **Instruction**: Use the module path from `go.mod` for all imports (e.g., `github.com/chrismichels/pr-monitor/internal/store`)
- **Added**: Seed

### Sign: Package structure follows daemon+TUI split design
- **Trigger**: Adding new functionality
- **Instruction**: Place code in the correct internal/ package:
  - `internal/config` — YAML config loading only
  - `internal/poller` — GitHub API polling + auth only
  - `internal/store` — SQLite CRUD and delta detection only
  - `internal/notify` — OSC toast and status JSON writing only
  - `internal/tui` — Bubble Tea UI only
  - `internal/discover` — filesystem repo scanning only
  Packages communicate through interfaces and the store, NOT direct imports of each other.
- **Added**: Seed — enables future daemon+TUI binary split

### Sign: Poller and TUI must not import each other
- **Trigger**: Writing import statements in poller or tui packages
- **Instruction**: These packages communicate through `internal/store` (shared SQLite). The poller writes data, the TUI reads data. This separation is critical for the future v2 daemon+TUI split.
- **Added**: Seed — architecture constraint

### Sign: Use modernc.org/sqlite, not mattn/go-sqlite3
- **Trigger**: Adding SQLite dependency
- **Instruction**: Use `modernc.org/sqlite` (pure Go, no CGo). Do NOT use `mattn/go-sqlite3`. The driver import path is `_ "modernc.org/sqlite"` and the `database/sql` driver name is `"sqlite"`.
- **Added**: Seed — CGo-free build requirement

### Sign: Auth comes from gh CLI
- **Trigger**: Needing a GitHub API token
- **Instruction**: Shell out to `gh auth token` to get the token. Do NOT hardcode tokens or read from environment variables.
- **Added**: Seed — decision Q7

### Sign: GitHub GraphQL via githubv4, not REST
- **Trigger**: Making GitHub API calls
- **Instruction**: Use `github.com/shurcooL/githubv4` for all GitHub queries. GraphQL lets us fetch review requests + authored PRs in minimal round trips.
- **Added**: Seed — tech stack decision

### Sign: OSC 9 escape sequences via /dev/tty, NOT stdout
- **Trigger**: Sending toast notifications
- **Instruction**: Use OSC 9 format: `\033]9;message\033\\` for wezterm toast. Write to `/dev/tty` (NOT stdout). Bubble Tea owns stdout for rendering — writing OSC sequences to stdout will corrupt the TUI. Open `/dev/tty` with `os.OpenFile("/dev/tty", os.O_WRONLY, 0)` in main.go and pass it to the Notifier as an `io.Writer`. In tests, use a `bytes.Buffer` instead.
- **Added**: Seed — decision Q3 + review item #1

### Sign: Bubble Tea for TUI
- **Trigger**: Building terminal UI components
- **Instruction**: Use `github.com/charmbracelet/bubbletea` for the TUI framework and `github.com/charmbracelet/lipgloss` for styling. Follow the Elm architecture (Model, Update, View).
- **Added**: Seed — tech stack decision

### Sign: Status JSON for wezterm integration
- **Trigger**: Updating PR counts or state for external consumption
- **Instruction**: Write status to the path from config (`notifications.status_json_path`, default `~/.config/pr-monitor/status.json`) after every poll cycle. Use atomic write (temp file + rename). Format: `{"review_count":N,"authored_activity_count":N,"last_updated":"...","oldest_review_age_hours":N}`
- **Added**: Seed — wezterm reads this file

### Sign: Use testify/assert for tests
- **Trigger**: Writing test files
- **Instruction**: Use `github.com/stretchr/testify/assert` for assertions. Table-driven tests where applicable. Do NOT use testify suites unless genuinely needed.
- **Added**: Seed

### Sign: Config is YAML at ~/.config/pr-monitor/config.yaml
- **Trigger**: Reading or writing configuration
- **Instruction**: Use `gopkg.in/yaml.v3` for config. Default location is `~/.config/pr-monitor/config.yaml`. Support `XDG_CONFIG_HOME` override. Expand `~` in all path fields.
- **Added**: Seed

### Sign: SQLite in-memory for tests
- **Trigger**: Writing tests that need a database
- **Instruction**: Use `:memory:` as the SQLite path in tests. Do NOT create temp files for test databases.
- **Added**: Seed

### Sign: Always run verification before committing
- **Trigger**: Before any git commit
- **Instruction**: Run `go build -o /dev/null ./... && go vet ./... && go test ./...`
- **Added**: Seed

### Sign: Mock GitHub API in tests
- **Trigger**: Writing tests for poller package
- **Instruction**: Do NOT make real GitHub API calls in tests. Use interface-based mocking or httptest for HTTP-level mocking.
- **Added**: Seed

### Sign: Exclude Dependabot PRs from review polling
- **Trigger**: Building GitHub search queries for review requests
- **Instruction**: Add `-author:app/dependabot` to all review request search queries. This filters Dependabot PRs server-side so they never enter the pipeline. Not needed for authored PR queries (the viewer is never dependabot).
- **Added**: Post-build — user requirement

### Sign: Run go mod tidy after adding imports
- **Trigger**: Adding a new import that introduces an external dependency
- **Instruction**: After adding any new external import (github.com/*, gopkg.in/*, modernc.org/*), run `go mod tidy` to fetch the dependency and update go.mod/go.sum. If `go build` fails with "missing module", this is why.
- **Added**: Review Item #13

### Sign: SQLite WAL mode for concurrent access
- **Trigger**: Opening the SQLite database
- **Instruction**: Enable WAL mode immediately after opening: `PRAGMA journal_mode=WAL`. This allows the poller goroutine to write while the TUI reads without blocking. Skip for `:memory:` databases in tests (WAL not supported for in-memory).
- **Added**: Review Item #11 — concurrent read/write

### Sign: Self-authored PRs are never "to review"
- **Trigger**: Processing review request poll results
- **Instruction**: If a PR returned by `FetchReviewRequests` was authored by the viewer (the authenticated user), DROP it from the review results. Standard Git rules: the submitter cannot review their own PR. The authored PR pipeline (`FetchAuthoredPRs`) will pick it up with `role = "author"`. This means the `UNIQUE(repo, number)` constraint in SQLite is safe — there will only ever be one row per PR.
- **Added**: Review Item #3 — dual-role dedup
