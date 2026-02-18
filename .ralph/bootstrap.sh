#!/bin/bash
# .ralph/bootstrap.sh — Phase 0: Bootstrap the pr-monitor project
#
# Creates the full project scaffold: go.mod, directory structure,
# stub package files, CLAUDE.md, and example config.
#
# This is NOT a Ralph loop — it's a one-shot setup script.
# Run this before starting Ralph phases.

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "=== Phase 0: Bootstrapping pr-monitor ==="
echo "  Project root: $PROJECT_ROOT"

# ── Step 0.1: Git init ──────────────────────────────────────────
if [ ! -d .git ]; then
  echo "--- Initializing git repository ---"
  git init
  cat > .gitignore << 'GITIGNORE'
# Binaries
bin/
pr-monitor
*.exe

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Build
dist/
coverage.out

# Runtime state (not committed)
state.db
status.json
GITIGNORE
  echo "  Created .gitignore"
else
  echo "--- Git already initialized ---"
fi

# ── Step 0.2: Go module init ────────────────────────────────────
if [ ! -f go.mod ]; then
  echo "--- Initializing Go module ---"
  go mod init github.com/chrismichels/pr-monitor
  echo "  Created go.mod"
else
  echo "--- go.mod already exists ---"
fi

# ── Step 0.3: Create directory structure ─────────────────────────
echo "--- Creating directory structure ---"
mkdir -p cmd/pr-monitor
mkdir -p internal/config
mkdir -p internal/poller
mkdir -p internal/store
mkdir -p internal/notify
mkdir -p internal/tui
mkdir -p internal/discover
mkdir -p wezterm

# ── Step 0.4: Create stub Go files ──────────────────────────────
echo "--- Creating stub Go files ---"

# cmd/pr-monitor/main.go
if [ ! -f cmd/pr-monitor/main.go ]; then
cat > cmd/pr-monitor/main.go << 'GO'
package main

import "fmt"

func main() {
	fmt.Println("pr-monitor: not yet implemented")
}
GO
  echo "  Created cmd/pr-monitor/main.go"
fi

# internal/config/config.go
if [ ! -f internal/config/config.go ]; then
cat > internal/config/config.go << 'GO'
package config
GO
  echo "  Created internal/config/config.go"
fi

# internal/poller/poller.go
if [ ! -f internal/poller/poller.go ]; then
cat > internal/poller/poller.go << 'GO'
package poller
GO
  echo "  Created internal/poller/poller.go"
fi

# internal/store/store.go
if [ ! -f internal/store/store.go ]; then
cat > internal/store/store.go << 'GO'
package store
GO
  echo "  Created internal/store/store.go"
fi

# internal/notify/notify.go
if [ ! -f internal/notify/notify.go ]; then
cat > internal/notify/notify.go << 'GO'
package notify
GO
  echo "  Created internal/notify/notify.go"
fi

# internal/tui/tui.go
if [ ! -f internal/tui/tui.go ]; then
cat > internal/tui/tui.go << 'GO'
package tui
GO
  echo "  Created internal/tui/tui.go"
fi

# internal/discover/discover.go
if [ ! -f internal/discover/discover.go ]; then
cat > internal/discover/discover.go << 'GO'
package discover
GO
  echo "  Created internal/discover/discover.go"
fi

# ── Step 0.4b: Create .claude/settings.json ───────────────────────
echo "--- Creating .claude/settings.json ---"
mkdir -p .claude
if [ ! -f .claude/settings.json ]; then
cat > .claude/settings.json << 'JSON'
{
  "permissions": {
    "allow": [
      "Bash(go build*)",
      "Bash(go test*)",
      "Bash(go vet*)",
      "Bash(go mod*)",
      "Bash(go run*)",
      "Bash(go install*)",
      "Bash(git *)",
      "Bash(make *)",
      "Bash(ls *)",
      "Bash(wc *)"
    ],
    "deny": []
  },
  "sandbox": {
    "enabled": true,
    "autoAllowBashIfSandboxed": true
  }
}
JSON
  echo "  Created .claude/settings.json"
else
  echo "  .claude/settings.json already exists"
fi

# ── Step 0.5: Create CLAUDE.md ──────────────────────────────────
echo "--- Creating CLAUDE.md ---"
cat > CLAUDE.md << 'CLAUDEMD'
# pr-monitor

## Ralph Loop Context

This project is being developed using Ralph Wiggum Loops.
- Task definitions: .ralph/phase*.md
- Guardrails: .ralph/guardrails.md (READ FIRST)
- Design reference: plans/pr-monitor-final.md
- Loop runner: plans/wiggum.md

## What This Tool Does

pr-monitor is a terminal-native GitHub PR monitoring tool. It:
1. Polls GitHub for PRs requiring user's review (personal + team-based)
2. Polls GitHub for activity on PRs the user authored (approvals, comments, change requests)
3. Persists state in SQLite, detects new events via delta comparison
4. Renders a Bubble Tea TUI with two tabs: "To Review" and "My PRs"
5. Fires OSC toast notifications for new events
6. Writes status JSON for wezterm status bar badge
7. Launches review workflow: wezterm pane → repo → claude-code → /review-pr

## Package Map

- `cmd/pr-monitor/` — entrypoint, wires everything
- `internal/config/` — YAML config loading
- `internal/poller/` — GitHub GraphQL polling + auth
- `internal/store/` — SQLite persistence + delta detection
- `internal/notify/` — OSC toast + status JSON writer
- `internal/tui/` — Bubble Tea interactive UI
- `internal/discover/` — local repo filesystem scanner (worktree-aware)

## Architecture Rule

**Poller and TUI do NOT import each other.** They communicate through Store (shared SQLite).
This enables a future split into separate daemon + TUI binaries.

Package dependency flow:
```
config ← (all packages read config)
poller → store (writes poll results)
store ← tui (reads for display)
store ← notify (reads for delta detection)
discover ← tui (resolves repo paths for review launch)
```

## Key Technical Decisions

- **SQLite**: Use `modernc.org/sqlite` (pure Go, no CGo). NOT `mattn/go-sqlite3`.
- **GitHub API**: GraphQL via `github.com/shurcooL/githubv4`. NOT REST.
- **Auth**: Shell out to `gh auth token`. NOT env vars or PAT files.
- **TUI**: `github.com/charmbracelet/bubbletea` + `lipgloss`.
- **Config**: YAML at `~/.config/pr-monitor/config.yaml` via `gopkg.in/yaml.v3`.
- **Notifications**: OSC 9 escape codes (`\033]9;msg\033\\`) to /dev/tty (NOT stdout — Bubble Tea owns stdout).

## GitHub Queries

Review requests (3 queries combined + deduplicated):
- `is:open is:pr review-requested:@me`
- `is:open is:pr team-review-requested:{org}/tsp-admin-contributors`
- `is:open is:pr team-review-requested:{org}/tsp-contributors`

Authored PR activity:
- `is:open is:pr author:@me` + `timelineItems` for review/comment events

## Verification

Always run before committing:
```
go build -o /dev/null ./... && go vet ./... && go test ./...
```

## Testing

- Use `github.com/stretchr/testify/assert` for assertions
- Use table-driven tests where applicable
- SQLite tests use in-memory database (`:memory:`)
- Mock GitHub API responses for poller tests (do NOT make real API calls in tests)
- Do NOT use testify suites unless shared setup is genuinely needed
CLAUDEMD
echo "  Created CLAUDE.md"

# ── Step 0.6: Create example config ─────────────────────────────
echo "--- Creating example config ---"
cat > config.example.yaml << 'YAML'
# pr-monitor configuration
# Copy to ~/.config/pr-monitor/config.yaml and edit

github:
  # GitHub org name (for team-based review request queries)
  org: "your-org"
  # Teams that trigger review monitoring (in addition to personal requests)
  review_teams:
    - "tsp-admin-contributors"
    - "tsp-contributors"
  # How often to poll GitHub (Go duration format)
  poll_interval: "3m"

# Directories to scan for locally cloned repos
workspace_dirs:
  - "/Volumes/data/projects"

# Explicit repo → local path overrides (optional)
# repo_overrides:
#   "org/repo-name": "/path/to/local/clone"

# Clone settings for repos not found locally
clone:
  default_dir: "/Volumes/data/projects"
  prompt_on_missing: true

# Shame timer color thresholds (hours since PR was created)
shame_timer:
  green: 4      # < 4h: green (fresh)
  yellow: 24    # 4-24h: yellow (aging)
  red: 48       # > 48h: red (overdue)

# Notification settings
notifications:
  toast_enabled: true
  status_json_path: "~/.config/pr-monitor/status.json"
YAML
echo "  Created config.example.yaml"

# ── Step 0.7: Verify build ──────────────────────────────────────
echo "--- Verifying build ---"
if go build -o /dev/null ./...; then
  echo "  go build: OK"
else
  echo "  go build: FAILED"
  exit 1
fi

if go vet ./...; then
  echo "  go vet: OK"
else
  echo "  go vet: FAILED"
  exit 1
fi

# ── Step 0.8: Commit scaffold ───────────────────────────────────
echo "--- Committing project scaffold ---"
git add -A
git commit -m "ralph: phase 0 — project scaffold

- go.mod initialized
- Directory structure: cmd/pr-monitor, internal/{config,poller,store,notify,tui,discover}
- Stub Go files for all packages
- CLAUDE.md with project context and architecture rules
- .ralph/ loop infrastructure (loop.sh, run-all.sh, phase files, guardrails)
- plans/ with design docs and wiggum loop plan
- Example config file"

echo ""
echo "=== Phase 0: Bootstrap complete ==="
echo "  Project structure created and committed."
echo "  Ready for Ralph loops: .ralph/run-all.sh 1"
