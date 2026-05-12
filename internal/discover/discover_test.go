package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// helper: create a fake regular clone with a .git/config containing the given remote URL.
func makeClone(t *testing.T, base, name, remoteURL string) string {
	t.Helper()
	repoDir := filepath.Join(base, name)
	gitDir := filepath.Join(repoDir, ".git")
	assert.NoError(t, os.MkdirAll(gitDir, 0o755))

	config := `[core]
	repositoryformatversion = 0
	filemode = true
[remote "origin"]
	url = ` + remoteURL + `
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
	merge = refs/heads/main
`
	assert.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o644))
	return repoDir
}

// helper: create a fake worktree directory with a .git file pointing to a parent repo's worktrees dir.
func makeWorktree(t *testing.T, base, name, mainRepoGitDir string) string {
	t.Helper()
	wtDir := filepath.Join(base, name)
	assert.NoError(t, os.MkdirAll(wtDir, 0o755))

	// Create the worktrees directory in the main repo's .git.
	wtGitDir := filepath.Join(mainRepoGitDir, "worktrees", name)
	assert.NoError(t, os.MkdirAll(wtGitDir, 0o755))

	// The .git file in the worktree points to the worktrees subdir.
	gitFileContent := "gitdir: " + wtGitDir + "\n"
	assert.NoError(t, os.WriteFile(filepath.Join(wtDir, ".git"), []byte(gitFileContent), 0o644))

	return wtDir
}

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{name: "SSH with .git", url: "git@github.com:org/repo.git", expected: "org/repo"},
		{name: "SSH without .git", url: "git@github.com:org/repo", expected: "org/repo"},
		{name: "HTTPS with .git", url: "https://github.com/org/repo.git", expected: "org/repo"},
		{name: "HTTPS without .git", url: "https://github.com/org/repo", expected: "org/repo"},
		{name: "HTTP with .git", url: "http://github.com/org/repo.git", expected: "org/repo"},
		{name: "non-github SSH", url: "git@gitlab.com:org/repo.git", expected: ""},
		{name: "non-github HTTPS", url: "https://gitlab.com/org/repo.git", expected: ""},
		{name: "empty string", url: "", expected: ""},
		{name: "malformed", url: "not-a-url", expected: ""},
		{name: "SSH no org", url: "git@github.com:repo.git", expected: ""},
		{name: "HTTPS extra path segments", url: "https://github.com/org/repo/tree/main", expected: "org/repo"},
		{name: "HTTPS with user@", url: "https://octocat@github.com/acme/sample-web.git", expected: "acme/sample-web"},
		{name: "HTTP with user@", url: "http://user@github.com/org/repo.git", expected: "org/repo"},
		{name: "HTTPS user@ non-github", url: "https://user@gitlab.com/org/repo.git", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGitHubRepo(tt.url)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestScanRegularCloneSSH(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "my-repo", "git@github.com:acme/my-repo.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	path, ok := idx.Resolve("acme/my-repo")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(workspace, "my-repo"), path)
}

func TestScanRegularCloneHTTPS(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "web-app", "https://github.com/acme/web-app.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	path, ok := idx.Resolve("acme/web-app")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(workspace, "web-app"), path)
}

func TestScanWorktree(t *testing.T) {
	workspace := t.TempDir()

	// Create the main clone first.
	mainRepo := makeClone(t, workspace, "main-repo", "git@github.com:acme/main-repo.git")
	mainGitDir := filepath.Join(mainRepo, ".git")

	// Create a worktree alongside the main repo.
	makeWorktree(t, workspace, "feature-branch", mainGitDir)

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	// Both should resolve to "acme/main-repo".
	path, ok := idx.Resolve("acme/main-repo")
	assert.True(t, ok)
	// The main clone has a shorter path, so it should be preferred.
	assert.Equal(t, filepath.Join(workspace, "main-repo"), path)
}

func TestScanWorktreeOnly(t *testing.T) {
	// Edge case: workspace only contains a worktree, main repo is elsewhere.
	mainDir := t.TempDir()
	wtWorkspace := t.TempDir()

	mainRepo := makeClone(t, mainDir, "parent", "git@github.com:acme/parent.git")
	mainGitDir := filepath.Join(mainRepo, ".git")

	makeWorktree(t, wtWorkspace, "wt-branch", mainGitDir)

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{wtWorkspace}))

	path, ok := idx.Resolve("acme/parent")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(wtWorkspace, "wt-branch"), path)
}

func TestOverridePrecedence(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "repo", "git@github.com:acme/repo.git")

	overrides := map[string]string{
		"acme/repo": "/custom/override/path",
	}
	idx := NewIndex(overrides)
	assert.NoError(t, idx.Scan([]string{workspace}))

	path, ok := idx.Resolve("acme/repo")
	assert.True(t, ok)
	assert.Equal(t, "/custom/override/path", path)
}

func TestOverrideForUndiscoveredRepo(t *testing.T) {
	overrides := map[string]string{
		"org/secret-repo": "/opt/repos/secret",
	}
	idx := NewIndex(overrides)
	// No scan at all.

	path, ok := idx.Resolve("org/secret-repo")
	assert.True(t, ok)
	assert.Equal(t, "/opt/repos/secret", path)
}

func TestResolveUnknownRepo(t *testing.T) {
	idx := NewIndex(nil)

	_, ok := idx.Resolve("unknown/repo")
	assert.False(t, ok)
}

func TestReposReturnsMerged(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "discovered", "git@github.com:acme/discovered.git")

	overrides := map[string]string{
		"acme/override": "/path/override",
	}
	idx := NewIndex(overrides)
	assert.NoError(t, idx.Scan([]string{workspace}))

	repos := idx.Repos()
	assert.Equal(t, filepath.Join(workspace, "discovered"), repos["acme/discovered"])
	assert.Equal(t, "/path/override", repos["acme/override"])
}

func TestDepthLimiting(t *testing.T) {
	workspace := t.TempDir()

	// Create a repo 5 levels deep — should NOT be found (exceeds maxScanDepth=4).
	deepPath := filepath.Join(workspace, "a", "b", "c", "d", "deep-repo")
	gitDir := filepath.Join(deepPath, ".git")
	assert.NoError(t, os.MkdirAll(gitDir, 0o755))
	config := `[remote "origin"]
	url = git@github.com:acme/deep-repo.git
`
	assert.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o644))

	// Create a repo 2 levels deep — should be found.
	makeClone(t, filepath.Join(workspace, "org"), "shallow-repo", "git@github.com:acme/shallow-repo.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	_, ok := idx.Resolve("acme/deep-repo")
	assert.False(t, ok, "repo at depth 5 should not be found")

	_, ok = idx.Resolve("acme/shallow-repo")
	assert.True(t, ok, "repo at depth 2 should be found")
}

func TestScanMultipleWorkspaceDirs(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()

	makeClone(t, ws1, "repo-a", "git@github.com:org1/repo-a.git")
	makeClone(t, ws2, "repo-b", "https://github.com/org2/repo-b.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{ws1, ws2}))

	_, ok := idx.Resolve("org1/repo-a")
	assert.True(t, ok)
	_, ok = idx.Resolve("org2/repo-b")
	assert.True(t, ok)
}

func TestScanSkipsHiddenDirectories(t *testing.T) {
	workspace := t.TempDir()

	// Create a repo inside a hidden directory — should be skipped.
	hiddenDir := filepath.Join(workspace, ".hidden")
	makeClone(t, hiddenDir, "secret", "git@github.com:acme/secret.git")

	// Create a visible repo.
	makeClone(t, workspace, "visible", "git@github.com:acme/visible.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	_, ok := idx.Resolve("acme/secret")
	assert.False(t, ok, "repos inside hidden dirs should be skipped")

	_, ok = idx.Resolve("acme/visible")
	assert.True(t, ok)
}

func TestScanNonGitHubRemote(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "gitlab-repo", "git@gitlab.com:acme/gitlab-repo.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	_, ok := idx.Resolve("acme/gitlab-repo")
	assert.False(t, ok, "non-github repos should not be indexed")
}

func TestScanPrefersShorterPath(t *testing.T) {
	workspace := t.TempDir()

	// Both clones have the same remote.
	makeClone(t, workspace, "repo", "git@github.com:acme/repo.git")
	makeClone(t, filepath.Join(workspace, "nested"), "repo-longer-name", "git@github.com:acme/repo.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	path, ok := idx.Resolve("acme/repo")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(workspace, "repo"), path, "should prefer shorter path")
}

func TestNewIndexCopiesOverrides(t *testing.T) {
	overrides := map[string]string{"a/b": "/path"}
	idx := NewIndex(overrides)

	// Mutating the original map should not affect the index.
	overrides["c/d"] = "/other"

	_, ok := idx.Resolve("c/d")
	assert.False(t, ok, "index should have its own copy of overrides")
}

func TestRescan_PicksUpNewRepo(t *testing.T) {
	workspace := t.TempDir()
	makeClone(t, workspace, "existing", "git@github.com:acme/existing.git")

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	_, ok := idx.Resolve("acme/existing")
	assert.True(t, ok, "existing repo should be found after initial scan")
	_, ok = idx.Resolve("acme/new-repo")
	assert.False(t, ok, "new repo should not exist yet")

	// Add a new repo after initial scan.
	makeClone(t, workspace, "new-repo", "git@github.com:acme/new-repo.git")

	assert.NoError(t, idx.Rescan())

	_, ok = idx.Resolve("acme/existing")
	assert.True(t, ok, "existing repo should survive rescan")
	_, ok = idx.Resolve("acme/new-repo")
	assert.True(t, ok, "new repo should be found after rescan")
}

func TestRescan_NoDirsIsNoop(t *testing.T) {
	idx := NewIndex(nil)
	// No Scan() called — dirs is empty.
	assert.NoError(t, idx.Rescan())
}

func TestWorktreeRelativeGitdir(t *testing.T) {
	workspace := t.TempDir()

	// Create main repo with a short name.
	mainRepo := makeClone(t, workspace, "repo", "git@github.com:acme/repo.git")
	mainGitDir := filepath.Join(mainRepo, ".git")

	// Create worktree with a longer name and a relative gitdir path.
	wtName := "repo-feature-long-branch"
	wtDir := filepath.Join(workspace, wtName)
	assert.NoError(t, os.MkdirAll(wtDir, 0o755))

	wtGitPath := filepath.Join(mainGitDir, "worktrees", wtName)
	assert.NoError(t, os.MkdirAll(wtGitPath, 0o755))

	// Compute relative path from wtDir to the worktrees dir.
	relPath, err := filepath.Rel(wtDir, wtGitPath)
	assert.NoError(t, err)

	gitFileContent := "gitdir: " + relPath + "\n"
	assert.NoError(t, os.WriteFile(filepath.Join(wtDir, ".git"), []byte(gitFileContent), 0o644))

	idx := NewIndex(nil)
	assert.NoError(t, idx.Scan([]string{workspace}))

	path, ok := idx.Resolve("acme/repo")
	assert.True(t, ok)
	// Should prefer the main clone (shorter path) over the worktree.
	assert.Equal(t, filepath.Join(workspace, "repo"), path)
}
