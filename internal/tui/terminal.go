package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	// SendLine types text into the target and presses Enter to execute it.
	SendLine(targetID, text string) error
}

// DetectDriver returns a TerminalDriver based on the environment.
// If driverName is "auto", it checks for $TMUX; otherwise it uses the
// explicit name ("tmux").
func DetectDriver(driverName string) (TerminalDriver, error) {
	switch driverName {
	case "tmux":
		return &TmuxDriver{}, nil
	case "", "auto":
		if os.Getenv("TMUX") != "" {
			return &TmuxDriver{}, nil
		}
		return nil, fmt.Errorf("not running inside tmux (pr-monitor requires tmux)")
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

func (d *TmuxDriver) SendLine(targetID, text string) error {
	// send-keys with a trailing Enter — executes immediately.
	return exec.Command("tmux", "send-keys", "-t", targetID, text, "Enter").Run()
}

