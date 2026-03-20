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

// wslPath resolves a Windows executable by checking PATH first, then falling
// back to the absolute path under /mnt/c/Windows/System32. This handles WSL2
// setups where the Windows System32 directory is not in $PATH.
func wslPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return "/mnt/c/Windows/System32/" + name
}

// clipboardCmd returns an exec.Cmd that accepts stdin and copies it to the
// system clipboard. Works on macOS, Linux (X11/Wayland), and WSL.
func clipboardCmd() *exec.Cmd {
	switch {
	case runtime.GOOS == "darwin":
		return exec.Command("pbcopy")
	case isWSL():
		// Prefer win32yank (commonly installed alongside neovim on WSL2),
		// then fall back to clip.exe via absolute path.
		if p, err := exec.LookPath("win32yank.exe"); err == nil {
			return exec.Command(p, "-i", "--crlf")
		}
		return exec.Command(wslPath("clip.exe"))
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
		// wslview is part of wslu; fall back to cmd.exe via absolute path
		// since /mnt/c/Windows/System32 is not always in $PATH on WSL2.
		if _, err := exec.LookPath("wslview"); err == nil {
			return exec.Command("wslview", url)
		}
		return exec.Command(wslPath("cmd.exe"), "/c", "start", "", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
