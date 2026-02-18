package poller

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// ResolveToken gets a GitHub token from the gh CLI.
// Shells out to `gh auth token` and returns the trimmed output.
func ResolveToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("gh CLI not found: install from https://cli.github.com")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("gh not authenticated: run `gh auth login` first (exit code %d)", exitErr.ExitCode())
		}
		return "", fmt.Errorf("failed to run gh auth token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned empty output: run `gh auth login` first")
	}
	return token, nil
}

// ValidateScopes checks that the token has the required scopes for pr-monitor.
// Returns a warning message if read:org is missing, nil if all good.
// This makes a single API call to GitHub.
func ValidateScopes(token string) error {
	return validateScopesWithURL(token, "https://api.github.com/")
}

func validateScopesWithURL(token, url string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach GitHub API: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("GitHub token is invalid or expired: run `gh auth login` to refresh")
	}

	scopes := resp.Header.Get("X-OAuth-Scopes")
	for _, scope := range strings.Split(scopes, ",") {
		if strings.TrimSpace(scope) == "read:org" {
			return nil
		}
	}
	return fmt.Errorf("GitHub token missing `read:org` scope (have: %s); team-based review requests will not work — run `gh auth refresh -s read:org` to fix", scopes)
}
