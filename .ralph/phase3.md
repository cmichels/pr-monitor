# Ralph Phase 3: Notifications + Delta Detection

> **Goal**: Wire the poll → store → notification pipeline.
> **Depends on**: Phase 1.2 (store) + Phase 2 (poller)
> **Design Reference**: `plans/pr-monitor-final.md`

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

---

## Task 3.1: Delta Detection and OSC Toast Notifications

Implement `internal/notify/toast.go` — emit OSC 9 escape sequences for
new PR events detected by comparing poll results against stored state.

### Context

- Target file: `internal/notify/toast.go` (new file in `internal/notify/`)
- The store's `FindNew()` method returns PRs where `notified_at IS NULL AND status = 'pending'`
- OSC 9 format: `\033]9;message\033\\`
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Implement `internal/notify/toast.go`:

```go
// Notifier sends OSC toast notifications to the terminal.
// IMPORTANT: The writer MUST be /dev/tty, NOT os.Stdout.
// Bubble Tea owns stdout for TUI rendering. Writing OSC to stdout
// will corrupt the display. /dev/tty bypasses Bubble Tea entirely.
// In main.go: tty, _ := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
// In tests: use bytes.Buffer
type Notifier struct {
    w       io.Writer // /dev/tty in prod, bytes.Buffer in tests
    enabled bool
}

func NewNotifier(w io.Writer, enabled bool) *Notifier

// NotifyNewReview sends a toast for a new review request.
func (n *Notifier) NotifyNewReview(pr PR) error

// NotifyActivity sends a toast for new activity on an authored PR.
func (n *Notifier) NotifyActivity(pr PR) error
```

2. Define a minimal PR type for the notify package (to avoid importing store):

```go
// PR contains the fields needed for notification formatting.
type PR struct {
    Repo             string
    Number           int
    Title            string
    Author           string
    LastActivityType string // "approved", "commented", "changes_requested"
    LastActivityBy   string
}
```

3. Toast message formats:
- New review request: `"PR Review: {repo} #{number} — {title} (by @{author})"`
- Approved: `"PR Approved: {repo} #{number} — {title} (by @{activity_by})"`
- Commented: `"PR Comment: {repo} #{number} — {title} (by @{activity_by})"`
- Changes requested: `"Changes Requested: {repo} #{number} — {title} (by @{activity_by})"`

4. OSC emission:
```go
func (n *Notifier) emit(message string) error {
    if !n.enabled {
        return nil
    }
    _, err := fmt.Fprintf(n.w, "\033]9;%s\033\\", message)
    return err
}
```

5. Write tests in `internal/notify/toast_test.go`:
- Use `bytes.Buffer` as the writer to capture output
- Test NotifyNewReview emits correct OSC sequence
- Test NotifyActivity for each activity type (approved, commented, changes_requested)
- Test that disabled notifier emits nothing
- Test message formatting for each case

### Success Criteria

1. [ ] `internal/notify/toast.go` implements Notifier with NotifyNewReview and NotifyActivity
2. [ ] OSC 9 format used: `\033]9;message\033\\`
3. [ ] All four message types formatted correctly
4. [ ] Disabled notifier emits nothing
5. [ ] `internal/notify/toast_test.go` tests all notification types and disabled state
6. [ ] `go build -o /dev/null ./...` succeeds
7. [ ] `go vet ./...` is clean
8. [ ] `go test ./internal/notify/...` — all tests pass

### Constraints

- Do NOT import any other internal/ packages (store, poller, config)
- Define a local PR struct in the notify package for the fields needed
- Use `io.Writer` interface for output (testable, not hardcoded to stdout)
- Use `fmt.Fprintf` for OSC emission — no external libraries
- Keep toast messages concise — wezterm truncates long notifications

### Delegation Strategy

- **Keep in main context**: All work — small, self-contained implementation

### Output

When all criteria are met, output: DONE

---

## Task 3.2: Status JSON Writer

Implement `internal/notify/status.go` — write a JSON status file that
wezterm's Lua config reads for the status bar badge.

### Context

- Target file: `internal/notify/status.go` (new file in `internal/notify/`)
- Wezterm reads this file on a timer to update the right-status bar
- Must use atomic writes (temp file + rename) to avoid partial reads
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Implement `internal/notify/status.go`:

```go
// Status represents the data written to the status JSON file.
type Status struct {
    ReviewCount          int       `json:"review_count"`
    AuthoredActivityCount int      `json:"authored_activity_count"`
    LastUpdated          time.Time `json:"last_updated"`
    OldestReviewAgeHours float64  `json:"oldest_review_age_hours"`
}

// WriteStatus atomically writes the status JSON file.
// Uses write-to-temp-then-rename to prevent wezterm from reading partial data.
func WriteStatus(path string, status Status) error
```

2. Atomic write strategy:
- Write to `{path}.tmp` first
- `os.Rename` the temp file to the final path
- This is atomic on POSIX filesystems (macOS included)
- Create parent directories if they don't exist (`os.MkdirAll`)

3. JSON format (compact, single line — wezterm reads it frequently):
```json
{"review_count":3,"authored_activity_count":2,"last_updated":"2026-02-18T14:30:00Z","oldest_review_age_hours":72.5}
```

4. Write tests in `internal/notify/status_test.go`:
- Test WriteStatus creates the file with correct JSON
- Test atomic write (verify no partial content by checking file validity after write)
- Test parent directory creation
- Test overwrite of existing file
- Test JSON field values are correct

### Success Criteria

1. [ ] `internal/notify/status.go` implements `Status` struct and `WriteStatus` function
2. [ ] Atomic write via temp file + rename
3. [ ] Parent directories created if missing
4. [ ] JSON output matches expected format
5. [ ] `internal/notify/status_test.go` covers write, overwrite, directory creation
6. [ ] `go build -o /dev/null ./...` succeeds
7. [ ] `go vet ./...` is clean
8. [ ] `go test ./internal/notify/...` — all tests pass

### Constraints

- Use `encoding/json` — no external JSON libraries
- Use `os.CreateTemp` in the same directory as the target (ensures same filesystem for rename)
- Do NOT import any other internal/ packages
- Expand `~` in the path before writing (caller should do this, but handle it defensively)

### Delegation Strategy

- **Keep in main context**: All work — straightforward file I/O

### Output

When all criteria are met, output: DONE
