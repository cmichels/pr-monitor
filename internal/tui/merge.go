package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// -- Messages -----------------------------------------------------------------

type mergePreflightDoneMsg struct {
	pr           PRItem
	branch       string
	blockReason  string // empty if pre-flight passes
	err          error  // non-nil if `gh pr view` itself failed
}

type mergedMsg struct {
	pr     PRItem
	branch string
}

type branchDeletedMsg struct {
	pr      PRItem
	pending bool // true if we gave up waiting and GitHub may still be deleting
}

type mergeErrMsg struct {
	phase string // "preflight" | "merge" | "branch-check"
	err   error
}

// -- Pre-flight ---------------------------------------------------------------

// preflightJSON is the shape we ask `gh pr view` to return.
type preflightJSON struct {
	Mergeable         string `json:"mergeable"`
	MergeStateStatus  string `json:"mergeStateStatus"`
	HeadRefName       string `json:"headRefName"`
	StatusCheckRollup []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"statusCheckRollup"`
}

// mergePreflight shells out to `gh pr view` and inspects mergeability + CI.
// Returns a mergePreflightDoneMsg — blockReason is empty on success.
func mergePreflight(pr PRItem) tea.Cmd {
	repo := pr.pr.Repo
	number := pr.pr.Number

	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", number),
			"--repo", repo,
			"--json", "mergeable,mergeStateStatus,headRefName,statusCheckRollup")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return mergePreflightDoneMsg{pr: pr, err: fmt.Errorf("gh pr view: %s", strings.TrimSpace(stderr.String()))}
		}

		var info preflightJSON
		if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
			return mergePreflightDoneMsg{pr: pr, err: fmt.Errorf("parse gh output: %w", err)}
		}

		reason := evaluatePreflight(info)
		return mergePreflightDoneMsg{
			pr:          pr,
			branch:      info.HeadRefName,
			blockReason: reason,
		}
	}
}

// evaluatePreflight returns a human-readable block reason, or "" if the PR is
// safe to merge. Exported in-package for tests.
func evaluatePreflight(info preflightJSON) string {
	for _, c := range info.StatusCheckRollup {
		if strings.EqualFold(c.Conclusion, "FAILURE") {
			return fmt.Sprintf("CI check failing: %s", c.Name)
		}
	}
	switch strings.ToUpper(info.MergeStateStatus) {
	case "DIRTY":
		return "merge conflicts"
	case "BLOCKED":
		return "blocked by branch protection (failing required checks or missing approvals)"
	case "BEHIND":
		return "branch is behind base — update required"
	}
	if strings.EqualFold(info.Mergeable, "CONFLICTING") {
		return "merge conflicts"
	}
	return ""
}

// -- Merge --------------------------------------------------------------------

// mergePR shells out `gh pr merge --squash`. Repo has auto-delete configured,
// so --delete-branch is omitted on purpose.
func mergePR(pr PRItem, branch string) tea.Cmd {
	repo := pr.pr.Repo
	number := pr.pr.Number

	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", number),
			"--repo", repo,
			"--squash")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return mergeErrMsg{phase: "merge", err: fmt.Errorf("%s", strings.TrimSpace(stderr.String()))}
		}
		return mergedMsg{pr: pr, branch: branch}
	}
}

// -- Branch deletion poll -----------------------------------------------------

// branchDeletePollInterval and maxAttempts govern how patiently we wait for
// GitHub's auto-delete to propagate.
const (
	branchDeletePollInterval = 1500 * time.Millisecond
	branchDeleteMaxAttempts  = 3
)

// checkBranchDeleted polls `gh api repos/.../branches/<branch>`. Exit code
// non-zero with "404"/"Not Found" in stderr means the branch is gone.
// If still present after maxAttempts, returns branchDeletedMsg{pending: true}.
func checkBranchDeleted(pr PRItem, branch string, attempt int) tea.Cmd {
	return tea.Tick(branchDeletePollInterval, func(time.Time) tea.Msg {
		repo := pr.pr.Repo
		cmd := exec.Command("gh", "api",
			fmt.Sprintf("repos/%s/branches/%s", repo, branch))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil && looksLikeNotFound(stderr.String()) {
			return branchDeletedMsg{pr: pr, pending: false}
		}
		if attempt+1 >= branchDeleteMaxAttempts {
			return branchDeletedMsg{pr: pr, pending: true}
		}
		// Re-tick for another attempt. We wrap a new Cmd in the message
		// channel via a nested tea.Tick invocation.
		return branchPollAgainMsg{pr: pr, branch: branch, attempt: attempt + 1}
	})
}

// branchPollAgainMsg is an internal trampoline so we can re-fire the tick.
type branchPollAgainMsg struct {
	pr      PRItem
	branch  string
	attempt int
}

func looksLikeNotFound(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "404") || strings.Contains(s, "not found")
}
