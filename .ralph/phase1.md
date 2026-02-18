# Ralph Phase 1: Foundation Packages

> **Goal**: Config loading, SQLite store, and GitHub auth — the three pillars everything else depends on.
> **Design Reference**: `plans/pr-monitor-final.md`

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
    "name": "SQLite store with schema and CRUD",
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

---

## Task 1.1: YAML Config Loader

Implement `internal/config/config.go` — loads, validates, and provides defaults
for the pr-monitor YAML configuration.

### Context

- Target file: `internal/config/config.go` (exists, currently just `package config`)
- Example config: `config.example.yaml` in project root
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define config structs in `internal/config/config.go`:

```go
type Config struct {
    GitHub        GitHubConfig      `yaml:"github"`
    WorkspaceDirs []string          `yaml:"workspace_dirs"`
    RepoOverrides map[string]string `yaml:"repo_overrides"`
    Clone         CloneConfig       `yaml:"clone"`
    ShameTimer    ShameTimerConfig  `yaml:"shame_timer"`
    Notifications NotificationConfig `yaml:"notifications"`
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
    Green  int `yaml:"green"`   // hours threshold
    Yellow int `yaml:"yellow"`
    Red    int `yaml:"red"`
}

type NotificationConfig struct {
    ToastEnabled   bool   `yaml:"toast_enabled"`
    StatusJSONPath string `yaml:"status_json_path"`
}
```

2. Implement these functions:
- `Load(path string) (*Config, error)` — read file, unmarshal YAML, apply defaults, validate
- `LoadDefault() (*Config, error)` — resolve default path (`~/.config/pr-monitor/config.yaml`, respecting `XDG_CONFIG_HOME`), call Load. If file doesn't exist, return config with all defaults.
- `DefaultConfig() *Config` — returns a Config with all default values applied
- Implement `UnmarshalYAML` for `GitHubConfig` to handle `poll_interval` as a duration string (e.g., "3m")

3. Default values:
- `poll_interval`: 3 minutes
- `shame_timer.green`: 4
- `shame_timer.yellow`: 24
- `shame_timer.red`: 48
- `notifications.toast_enabled`: true
- `notifications.status_json_path`: `~/.config/pr-monitor/status.json`
- `clone.prompt_on_missing`: true

4. Validation:
- `github.org` is required (non-empty) if any `review_teams` are configured
- At least one `workspace_dirs` entry should exist
- All paths should have `~` expanded to the user's home directory

5. Write tests in `internal/config/config_test.go`:
- Test loading valid YAML config
- Test defaults are applied for missing fields
- Test LoadDefault when no config file exists (returns defaults)
- Test validation errors (missing org with teams configured)
- Test `~` expansion in paths
- Test poll_interval duration parsing

### Success Criteria

1. [ ] `internal/config/config.go` defines all config structs with yaml tags
2. [ ] `Load`, `LoadDefault`, `DefaultConfig` functions implemented
3. [ ] Defaults applied for all optional fields
4. [ ] Validation rejects missing org when review_teams is set
5. [ ] `~` expanded in all path fields
6. [ ] `internal/config/config_test.go` covers valid, default, missing file, validation, and path expansion cases
7. [ ] `go build -o /dev/null ./...` succeeds
8. [ ] `go vet ./...` is clean
9. [ ] `go test ./internal/config/...` — all tests pass

### Constraints

- Use `gopkg.in/yaml.v3` — no other YAML libraries
- Do NOT import any other internal/ packages
- Expand `~` using `os.UserHomeDir()`, not env var lookup
- Do NOT create the config directory or file — just read if it exists

### Delegation Strategy

- **Keep in main context**: All work — config loading is straightforward

### Output

When all criteria are met, output: DONE

---

## Task 1.2: SQLite Store with Schema and CRUD

Implement `internal/store/store.go` — SQLite database with schema creation,
PR upsert, query, status management, and delta detection.

### Context

- Target file: `internal/store/store.go` (exists, currently just `package store`)
- Schema design: `plans/pr-monitor-final.md` "Data Model" section
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define the PR struct in `internal/store/store.go`:

```go
type PR struct {
    ID               int64
    PRID             string    // GitHub node ID (globally unique)
    Repo             string    // "org/repo-name"
    Number           int
    Title            string
    Author           string
    URL              string    // HTML URL
    Role             string    // "reviewer" or "author"
    FilesChanged     int
    CIStatus         string    // "passing", "failing", "pending", "unknown"
    FirstSeen        time.Time
    LastSeen         time.Time
    NotifiedAt       *time.Time
    Status           string    // "pending", "dismissed", "reviewed"
    LastActivityAt   *time.Time
    LastActivityType *string   // "approved", "commented", "changes_requested"
    LastActivityBy   *string
}
```

2. Implement Store:

```go
type Store struct {
    db *sql.DB
}

func New(dbPath string) (*Store, error)  // open DB, enable WAL mode, create schema, return Store
func (s *Store) Close() error

// Write operations
func (s *Store) UpsertPR(ctx context.Context, pr PR) error
func (s *Store) Dismiss(ctx context.Context, prID string) error
func (s *Store) MarkReviewed(ctx context.Context, prID string) error
func (s *Store) MarkNotified(ctx context.Context, prID string) error
func (s *Store) UpdateActivity(ctx context.Context, prID string, activityAt time.Time, activityType, activityBy string) error
// UpdateActivity sets last_activity_* fields. If activityAt is newer than
// the current notified_at, it also clears notified_at so the PR appears
// in FindNew() again and triggers a re-notification for the new event.

// Read operations
func (s *Store) GetPendingByRole(ctx context.Context, role string) ([]PR, error)
func (s *Store) FindNew(ctx context.Context, role string) ([]PR, error)  // notified_at IS NULL AND status = 'pending'
func (s *Store) GetCounts(ctx context.Context) (reviewCount, authoredCount int, err error)
func (s *Store) OldestPendingAge(ctx context.Context) (time.Duration, error)

// Maintenance
func (s *Store) Cleanup(ctx context.Context, currentPRIDs []string) error  // remove PENDING PRs not in the current set (merged/closed). Dismissed PRs are preserved.
```

3. Enable WAL mode immediately after opening the connection:
```go
_, err := db.Exec("PRAGMA journal_mode=WAL")
```
WAL mode allows the poller goroutine to write while the TUI goroutine reads concurrently without blocking. This is critical for a smooth TUI experience. Note: WAL mode is not supported for `:memory:` databases, so skip this pragma when the path is `:memory:`.

4. Schema (create in `New` using `CREATE TABLE IF NOT EXISTS`):
   - Note: No migration system for MVP. Add a `// TODO: v2 — add schema_version table and migration logic` comment above the CREATE TABLE. For now, users can delete the DB file to reset if the schema changes during development.

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

4. UpsertPR behavior:
- If PR exists (by pr_id): update title, author, files_changed, ci_status, last_seen, url
- If PR does not exist: insert with all fields, first_seen = now, status = 'pending'
- Do NOT overwrite status, notified_at, or first_seen on update

5. Write tests in `internal/store/store_test.go`:
- Use `:memory:` SQLite for all tests
- Test New creates schema successfully
- Test UpsertPR insert and update paths
- Test GetPendingByRole filters by role and status
- Test FindNew returns only un-notified pending PRs
- Test Dismiss changes status
- Test MarkNotified sets notified_at
- Test UpdateActivity clears notified_at when new activity is more recent (enables re-notification)
- Test UpdateActivity does NOT clear notified_at when activity is older than last notification
- Test Cleanup removes only pending PRs not in current set (dismissed PRs are preserved)
- Test Cleanup does NOT remove dismissed PRs
- Test GetCounts returns correct counts
- Test OldestPendingAge returns correct duration

### Success Criteria

1. [ ] `internal/store/store.go` defines PR struct and Store type
2. [ ] `New` creates schema on first open
3. [ ] All CRUD methods implemented
4. [ ] UpsertPR correctly handles insert vs update (preserves first_seen, status)
5. [ ] `internal/store/store_test.go` covers all methods with in-memory SQLite
6. [ ] `go build -o /dev/null ./...` succeeds
7. [ ] `go vet ./...` is clean
8. [ ] `go test ./internal/store/...` — all tests pass

### Constraints

- Use `modernc.org/sqlite` — import as `_ "modernc.org/sqlite"`, driver name is `"sqlite"`
- Use `database/sql` standard library for all DB operations
- Use `:memory:` for tests — no temp files
- Do NOT import any other internal/ packages
- Use `context.Context` on all DB methods
- Handle nullable fields (`NotifiedAt`, `LastActivityAt`, etc.) with `*time.Time` / `*string` and `sql.NullTime` / `sql.NullString` for scanning

### Delegation Strategy

- **Delegate to subagents**: Check `modernc.org/sqlite` driver registration name and connection string format
- **Keep in main context**: Write store.go and store_test.go

### Output

When all criteria are met, output: DONE

---

## Task 1.3: GitHub Auth via gh CLI

Implement GitHub authentication in `internal/poller/auth.go` — resolve a
token from the `gh` CLI and optionally validate its scopes.

### Context

- Target file: `internal/poller/auth.go` (new file in `internal/poller/`)
- The poller package already has `poller.go` (stub)
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Implement `internal/poller/auth.go`:

```go
// ResolveToken gets a GitHub token from the gh CLI.
// Shells out to `gh auth token` and returns the trimmed output.
func ResolveToken() (string, error)
```

- Execute `gh auth token` via `os/exec`
- Trim whitespace from output
- Return clear error messages: "gh not found" vs "gh not authenticated"

2. Implement scope validation (best-effort, not blocking):

```go
// ValidateScopes checks that the token has the required scopes for pr-monitor.
// Returns a warning message if read:org is missing, nil if all good.
// This makes a single API call to GitHub.
func ValidateScopes(token string) error
```

- Make a GET request to `https://api.github.com/` with the token
- Check the `X-OAuth-Scopes` response header for `read:org`
- Return a descriptive error if `read:org` is missing (needed for team review request queries)
- This is a warning, not a hard failure — the tool can still work for personal review requests

3. Write tests in `internal/poller/auth_test.go`:
- Test ResolveToken parses output correctly (mock exec is optional — can test the parsing logic)
- Test ValidateScopes with mock HTTP server returning various scope headers
- Test error handling: missing scope, network error

### Success Criteria

1. [ ] `internal/poller/auth.go` implements `ResolveToken` and `ValidateScopes`
2. [ ] ResolveToken shells out to `gh auth token` and trims output
3. [ ] ValidateScopes checks `X-OAuth-Scopes` header for `read:org`
4. [ ] `internal/poller/auth_test.go` tests parsing and scope validation
5. [ ] `go build -o /dev/null ./...` succeeds
6. [ ] `go vet ./...` is clean
7. [ ] `go test ./internal/poller/...` — all tests pass

### Constraints

- Use `os/exec` for shelling out — no third-party exec libraries
- Use `net/http` for the scope check — no GitHub client library needed here
- Do NOT import any other internal/ packages
- Tests must NOT shell out to real `gh` or make real HTTP calls (use httptest)

### Delegation Strategy

- **Keep in main context**: All work — small, focused implementation

### Output

When all criteria are met, output: DONE
