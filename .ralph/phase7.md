# Ralph Phase 7: Polish

> **Goal**: Error handling, first-run UX, build tooling.
> **Depends on**: Phase 6 (everything wired)
> **Design Reference**: `plans/pr-monitor-final.md`

```json
[
  {
    "id": "7.1",
    "name": "Error handling, retries, and first-run UX",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./...",
    "max_iterations": 10
  },
  {
    "id": "7.2",
    "name": "Makefile and build tooling",
    "test_command": "make build && make test",
    "max_iterations": 5
  }
]
```

---

## Task 7.1: Error Handling, Retries, and First-Run UX

Harden the application with proper error handling, retry logic for
transient failures, and a friendly first-run experience.

### Context

- All packages are implemented and wired (Phases 1-6)
- This task adds robustness without changing core functionality
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. **GitHub rate limit handling** in `internal/poller/`:
- After each GraphQL query, check the rate limit response data
- If remaining < 10: log a warning and extend the poll interval temporarily
- If rate limited (403): log the reset time, sleep until reset + buffer
- Add to Poller:
```go
func (p *Poller) checkRateLimit(remaining int, resetAt time.Time) time.Duration
```

2. **Network retry with backoff** in poll cycle:
- If a poll fails due to network error: retry up to 3 times with exponential backoff (1s, 2s, 4s)
- If all retries fail: log the error, continue to next poll cycle (don't crash)
- Use a simple retry helper:
```go
func retry(ctx context.Context, maxAttempts int, fn func() error) error
```

3. **Auth expiration detection**:
- If GitHub returns 401: log a clear message "GitHub token expired. Run: gh auth login"
- Show this in the TUI status line as well
- Don't crash — keep the TUI running but show an error state

4. **First-run experience** in `cmd/pr-monitor/main.go`:
- If `~/.config/pr-monitor/` doesn't exist: create it
- If `config.yaml` doesn't exist: create it from defaults with a comment header
- Print to stderr on first run:
  ```
  pr-monitor: first run detected
    Created config: ~/.config/pr-monitor/config.yaml
    Edit the config to set your GitHub org and review teams.
  ```
- If `gh auth token` fails: print clear fix instructions and exit

5. **TUI error display**:
- Add an error state to the TUI model that shows inline error messages
- Errors auto-dismiss after 10 seconds or on any keypress
- Network errors: "GitHub API unreachable — retrying..."
- Auth errors: "GitHub token expired — run: gh auth login"
- Rate limit: "Rate limited — next poll in {duration}"

6. **Logging**:
- Use `log/slog` (Go 1.21+ structured logging) for all log output
- Log to stderr (TUI uses stdout)
- Log level: info by default, debug with `--debug` flag
- Key log events: poll start/complete, new PRs found, errors, rate limits

### Success Criteria

1. [ ] Rate limit detection and backoff implemented
2. [ ] Network retry with exponential backoff (max 3 attempts)
3. [ ] Auth expiration shows clear error message
4. [ ] First-run creates config directory and default config
5. [ ] TUI shows inline error messages for transient failures
6. [ ] Structured logging via `log/slog` to stderr
7. [ ] `--debug` flag enables debug-level logging
8. [ ] `go build -o /dev/null ./...` succeeds
9. [ ] `go vet ./...` is clean
10. [ ] `go test ./...` — all tests pass

### Constraints

- Use `log/slog` — no third-party logging libraries
- Log to stderr, never stdout (TUI owns stdout)
- Do NOT crash on transient errors — always recover and continue
- First-run config creation writes a commented YAML file, not just empty
- Retry logic must respect context cancellation

### Delegation Strategy

- **Delegate to subagents**: Read `log/slog` docs for structured logging patterns, check Go retry patterns
- **Keep in main context**: Implement error handling, retry, logging, first-run logic

### Output

When all criteria are met, output: DONE

---

## Task 7.2: Makefile and Build Tooling

Create a Makefile with standard targets for building, testing, installing,
and releasing pr-monitor.

### Context

- Target file: `Makefile` (new file in project root)
- Binary name: `pr-monitor`
- Binary output: `bin/pr-monitor`
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Create `Makefile`:

```makefile
.PHONY: build test lint install clean run

# Build variables
BINARY := pr-monitor
BUILD_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Default target
all: lint test build

# Build the binary
build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/pr-monitor

# Run all tests
test:
	go test ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Run linting
lint:
	go vet ./...

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/pr-monitor

# Run locally
run: build
	./$(BUILD_DIR)/$(BINARY)

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR) coverage.out
```

2. Add version injection to `cmd/pr-monitor/main.go`:

```go
var version = "dev"

// Used with --version flag
```

Ensure the `--version` flag prints this value.

3. Verify the build pipeline:
- `make build` produces `bin/pr-monitor`
- `make test` runs all tests
- `make lint` runs go vet
- `make install` installs to GOPATH/bin
- `make clean` removes artifacts

### Success Criteria

1. [ ] `Makefile` exists with build, test, lint, install, clean, run targets
2. [ ] Version injected via ldflags from git tags
3. [ ] `make build` produces `bin/pr-monitor`
4. [ ] `make test` passes all tests
5. [ ] `make lint` passes
6. [ ] `make clean` removes build artifacts

### Constraints

- Use GNU Make syntax (compatible with macOS default make)
- Use `.PHONY` for all targets
- Version falls back to "dev" if no git tags exist
- Do NOT add complex release tooling (goreleaser, etc.) — keep it simple

### Delegation Strategy

- **Keep in main context**: All work — Makefile is straightforward

### Output

When all criteria are met, output: DONE
