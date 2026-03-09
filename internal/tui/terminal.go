package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// TerminalDriver abstracts terminal multiplexer operations so the TUI can
// spawn windows/panes, set titles, and send keystrokes without being coupled
// to a specific terminal emulator.
type TerminalDriver interface {
	// SpawnWindow opens a new window/tab with the given working directory.
	// Returns an identifier (pane ID / window ID) for subsequent calls.
	SpawnWindow(cwd string) (targetID string, err error)
	// SetTitle sets the title of the window/tab identified by targetID.
	SetTitle(targetID, title string) error
	// SendText types text into the target without executing it (no Enter).
	SendText(targetID, text string) error
}

// DetectDriver returns a TerminalDriver based on the environment.
// If driverName is "auto", it checks env vars; otherwise it uses the
// explicit name ("tmux" or "wezterm").
func DetectDriver(driverName string) (TerminalDriver, error) {
	switch driverName {
	case "tmux":
		return &TmuxDriver{}, nil
	case "wezterm":
		return &WeztermDriver{}, nil
	case "", "auto":
		if os.Getenv("TMUX") != "" {
			return &TmuxDriver{}, nil
		}
		if os.Getenv("TERM_PROGRAM") == "WezTerm" {
			return &WeztermDriver{}, nil
		}
		return nil, fmt.Errorf("could not detect terminal multiplexer (set terminal: tmux or wezterm in config)")
	default:
		return nil, fmt.Errorf("unknown terminal driver: %q", driverName)
	}
}

// TmuxDriver implements TerminalDriver using tmux CLI commands.
type TmuxDriver struct{}

func (d *TmuxDriver) SpawnWindow(cwd string) (string, error) {
	// -P -F prints the new window's target in the format "#{window_id}".
	args := []string{"new-window", "-P", "-F", "#{window_id}"}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *TmuxDriver) SetTitle(targetID, title string) error {
	return exec.Command("tmux", "rename-window", "-t", targetID, title).Run()
}

func (d *TmuxDriver) SendText(targetID, text string) error {
	// send-keys without a trailing Enter — user reviews before executing.
	return exec.Command("tmux", "send-keys", "-t", targetID, text).Run()
}

// WeztermDriver implements TerminalDriver using wezterm CLI commands.
type WeztermDriver struct{}

func (d *WeztermDriver) SpawnWindow(cwd string) (string, error) {
	args := []string{"cli", "spawn"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	out, err := exec.Command("wezterm", args...).Output()
	if err != nil {
		return "", fmt.Errorf("wezterm cli spawn: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *WeztermDriver) SetTitle(targetID, title string) error {
	return exec.Command("wezterm", "cli", "set-tab-title", "--pane-id", targetID, title).Run()
}

func (d *WeztermDriver) SendText(targetID, text string) error {
	// Wezterm needs a delay for the shell to initialize before sending text.
	time.Sleep(1500 * time.Millisecond)
	return exec.Command("wezterm", "cli", "send-text", "--no-paste", "--pane-id", targetID, text).Run()
}
