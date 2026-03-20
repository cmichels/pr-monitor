package notify

import (
	"fmt"
	"io"
	"os"
)

// PR contains the fields needed for notification formatting.
// This is a local type to avoid importing internal/store.
type PR struct {
	Repo             string
	Number           int
	Title            string
	Author           string
	LastActivityType string // "approved", "commented", "changes_requested"
	LastActivityBy   string
}

// Notifier sends OSC 9 toast notifications to the terminal.
//
// The writer MUST be /dev/tty, NOT os.Stdout. Bubble Tea owns stdout for TUI
// rendering. Writing OSC to stdout will corrupt the display. /dev/tty bypasses
// Bubble Tea entirely.
//
// In main.go: tty, _ := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
// In tests: use bytes.Buffer
type Notifier struct {
	w       io.Writer
	enabled bool
}

// NewNotifier creates a Notifier that writes OSC 9 sequences to w.
func NewNotifier(w io.Writer, enabled bool) *Notifier {
	return &Notifier{w: w, enabled: enabled}
}

// NotifyNewReview sends a toast for a new review request.
func (n *Notifier) NotifyNewReview(pr PR) error {
	msg := fmt.Sprintf("PR Review: %s #%d — %s (by @%s)", pr.Repo, pr.Number, pr.Title, pr.Author)
	return n.emit(msg)
}

// NotifyActivity sends a toast for new activity on an authored PR.
func (n *Notifier) NotifyActivity(pr PR) error {
	var prefix string
	switch pr.LastActivityType {
	case "approved":
		prefix = "PR Approved"
	case "commented":
		prefix = "PR Comment"
	case "changes_requested":
		prefix = "Changes Requested"
	default:
		prefix = "PR Activity"
	}
	msg := fmt.Sprintf("%s: %s #%d — %s (by @%s)", prefix, pr.Repo, pr.Number, pr.Title, pr.LastActivityBy)
	return n.emit(msg)
}

// emit writes an OSC 9 escape sequence with the given message.
// When running inside tmux, the sequence is wrapped in a DCS passthrough so
// tmux forwards it to the outer terminal (Ghostty). Each ESC byte in the inner
// sequence must be doubled per the tmux passthrough spec.
func (n *Notifier) emit(message string) error {
	if !n.enabled {
		return nil
	}
	var err error
	if os.Getenv("TMUX") != "" {
		// \033Ptmux; — DCS passthrough start
		// \033\033]9;msg — doubled-ESC OSC 9
		// \033\033\\ — doubled-ESC String Terminator
		// \033\\ — DCS terminator
		_, err = fmt.Fprintf(n.w, "\033Ptmux;\033\033]9;%s\033\033\\\033\\", message)
	} else {
		_, err = fmt.Fprintf(n.w, "\033]9;%s\033\\", message)
	}
	return err
}
