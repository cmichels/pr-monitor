package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// isWSL returns true if running inside Windows Subsystem for Linux.
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

// clipboardCmd returns an exec.Cmd that accepts stdin and copies it to the
// system clipboard. Works on macOS, Linux (X11/Wayland), and WSL.
func clipboardCmd() *exec.Cmd {
	switch {
	case runtime.GOOS == "darwin":
		return exec.Command("pbcopy")
	case isWSL():
		return exec.Command("clip.exe")
	default:
		// Prefer xclip, fall back to xsel.
		if _, err := exec.LookPath("xclip"); err == nil {
			return exec.Command("xclip", "-selection", "clipboard")
		}
		return exec.Command("xsel", "--clipboard", "--input")
	}
}

// browserCmd returns an exec.Cmd that opens the given URL in the default browser.
func browserCmd(url string) *exec.Cmd {
	switch {
	case runtime.GOOS == "darwin":
		return exec.Command("open", url)
	case isWSL():
		// wslview is part of wslu, widely available in WSL distros.
		// Fall back to cmd.exe /c start if wslview isn't installed.
		if _, err := exec.LookPath("wslview"); err == nil {
			return exec.Command("wslview", url)
		}
		return exec.Command("cmd.exe", "/c", "start", "", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
