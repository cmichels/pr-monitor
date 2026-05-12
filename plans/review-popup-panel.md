# Plan: PR Review Popup Panel

## Goal

Route PR review tmux windows into a dedicated `pr-review` session (off the main status bar) and expose it via a right-side `display-popup` toggle bound to `prefix + G`.

## Context

Currently, `TmuxDriver.SpawnWindow` calls `tmux new-window` in the current session. Every review, quick-review, and address-comments window claims a status bar slot. This gets noisy fast when juggling multiple PRs.

**Approach:** Create a separate tmux session for review windows. Access it via `display-popup` — a modal overlay that captures input (including prefix keys) so you can navigate review tabs inside the popup without conflicting with the outer session.

## Scope

| In scope | Out of scope |
|---|---|
| `launchReview` (team review) | `launchWorktree` (Jira — stays main session) |
| `launchQuickReview` (quick review) | `launchTmuxForTask` (task resume — stays main session) |
| `addressComments` (PR feedback) | |

## Changes

### 1. TerminalDriver interface — `internal/tui/terminal.go`

Add a new method to the interface:

```go
// SpawnWindowInSession creates a window in a named session (creating the
// session if it doesn't exist). Returns the window ID.
SpawnWindowInSession(session, cwd string) (targetID string, err error)
```

**TmuxDriver implementation:**

```go
func (d *TmuxDriver) SpawnWindowInSession(session, cwd string) (string, error) {
    // Lazily create the session on first review
    if err := exec.Command("tmux", "has-session", "-t", session).Run(); err != nil {
        // Session doesn't exist — create detached with first window
        args := []string{"new-session", "-d", "-s", session, "-P", "-F", "#{window_id}"}
        if cwd != "" {
            args = append(args, "-c", cwd)
        }
        out, err := exec.Command("tmux", args...).Output()
        if err != nil {
            return "", fmt.Errorf("tmux new-session: %w", err)
        }
        return strings.TrimSpace(string(out)), nil
    }

    // Session exists — add window to it
    args := []string{"new-window", "-t", session + ":", "-P", "-F", "#{window_id}"}
    if cwd != "" {
        args = append(args, "-c", cwd)
    }
    out, err := exec.Command("tmux", args...).Output()
    if err != nil {
        return "", fmt.Errorf("tmux new-window: %w", err)
    }
    return strings.TrimSpace(string(out)), nil
}
```

**Key detail:** First review call creates the session + window in one shot (`new-session`). Subsequent reviews add windows (`new-window -t pr-review:`). No orphan empty windows.

### 2. Config — `internal/config/config.go`

Add a `Tmux` section to the config:

```yaml
tmux:
  review_session: "pr-review"   # session name for review windows
```

```go
type TmuxConfig struct {
    ReviewSession string `yaml:"review_session"`
}
```

Default: `"pr-review"`. Thread through to the Model so launch functions can read it.

Update `DefaultConfig()`, `applyDefaults()`, and `config.example.yaml`.

### 3. Review launch functions — `internal/tui/keys.go`

**`launchReviewWindow`** (line 131): Change `driver.SpawnWindow(path)` to `driver.SpawnWindowInSession(reviewSession, path)` where `reviewSession` comes from config.

**`addressComments`** (line 183): Same change — `driver.SpawnWindowInSession(reviewSession, path)`.

No changes to `launchWorktree` or `launchTmuxForTask` — they stay in the main session.

### 4. tmux keybind — `~/.tmux.conf` (os-setup dotfiles)

```bash
# Toggle PR review panel (right-side popup)
bind G display-popup -E -w 50% -h 100% \
  "tmux attach -t pr-review 2>/dev/null || { echo 'No active reviews'; read; }"
```

- `G` pairs with `g` (pr-monitor launcher) — mnemonic: `g` = monitor, `G` = reviews
- `-E` closes popup when inner session detaches or shell exits
- `-w 50% -h 100%` — half-width, full-height panel
- Falls back to a message if no reviews are running yet

### 5. Tests — `internal/tui/terminal_test.go`

- Test `SpawnWindowInSession` creates session on first call (mock `has-session` failing)
- Test `SpawnWindowInSession` adds window when session exists (mock `has-session` succeeding)
- Test that `SpawnWindow` (original) is unaffected

## File Summary

| File | Change |
|---|---|
| `internal/tui/terminal.go` | Add `SpawnWindowInSession` to interface + `TmuxDriver` |
| `internal/config/config.go` | Add `TmuxConfig` struct, defaults, validation |
| `config.example.yaml` | Add `tmux.review_session` example |
| `internal/tui/keys.go` | `launchReviewWindow` + `addressComments` use new method |
| `internal/tui/model.go` | Thread `reviewSession` config to Model |
| `internal/tui/terminal_test.go` | Unit tests for session-aware spawning |
| `~/.tmux.conf` (os-setup) | `bind G display-popup ...` keybind |

## Open Questions

1. **Popup positioning:** `display-popup` centers by default. If right-alignment is needed, test whether tmux 3.3a supports positional flags (e.g., `-x R`). Centered at 50% width is already a solid right-panel feel.
2. **Session cleanup:** When the last review window closes, the `pr-review` session lingers empty. Options:
   - Leave it (harmless — popup just shows an empty shell)
   - Set `destroy-unattached on` for the session so it dies when the popup closes and no windows remain
   - Add a `set-hook` on `window-closed` to kill the session if empty
3. **Popup width:** 50% is a starting point. May want to make this configurable or test what feels right for review workflows (Claude output can be wide).
