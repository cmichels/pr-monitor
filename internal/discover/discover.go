package discover

import (
	"bufio"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// Index maps GitHub repository names ("org/repo") to local filesystem paths.
type Index struct {
	repos     map[string]string // discovered: "org/repo" → "/local/path"
	overrides map[string]string // from config, takes precedence
}

// NewIndex creates a new Index with the given config overrides.
// Overrides always take precedence over discovered repos.
func NewIndex(overrides map[string]string) *Index {
	o := make(map[string]string, len(overrides))
	maps.Copy(o, overrides)
	return &Index{
		repos:     make(map[string]string),
		overrides: o,
	}
}

// maxScanDepth limits how deep we recurse into workspace directories.
const maxScanDepth = 4

// Scan walks the given directories looking for git repos and builds the index.
func (idx *Index) Scan(dirs []string) error {
	for _, dir := range dirs {
		if err := idx.scanDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// Resolve returns the local path for a GitHub repo, or false if not found.
// Config overrides take precedence over discovered paths.
func (idx *Index) Resolve(repo string) (string, bool) {
	if p, ok := idx.overrides[repo]; ok {
		return p, true
	}
	if p, ok := idx.repos[repo]; ok {
		return p, true
	}
	return "", false
}

// Repos returns all known repo → path mappings (overrides merged on top of discovered).
func (idx *Index) Repos() map[string]string {
	merged := make(map[string]string, len(idx.repos)+len(idx.overrides))
	maps.Copy(merged, idx.repos)
	maps.Copy(merged, idx.overrides)
	return merged
}

// scanDir walks a single workspace directory up to maxScanDepth levels deep.
func (idx *Index) scanDir(root string) error {
	root = filepath.Clean(root)

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip directories we can't read (permissions, etc.)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Only inspect directories.
		if !d.IsDir() {
			return nil
		}

		// Enforce depth limit relative to root.
		if depth(root, path) > maxScanDepth {
			return fs.SkipDir
		}

		// Skip hidden directories (except root itself).
		if path != root && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}

		// Check for a .git entry inside this directory.
		dotGit := filepath.Join(path, ".git")
		info, statErr := os.Stat(dotGit)
		if statErr != nil {
			// No .git here — keep walking.
			return nil
		}

		repo := ""
		if info.IsDir() {
			// Case A: regular clone — .git is a directory.
			repo = repoFromGitConfig(filepath.Join(dotGit, "config"))
		} else {
			// Case B: worktree — .git is a file pointing to the main repo.
			repo = repoFromWorktreeGitFile(dotGit)
		}

		if repo != "" {
			idx.addRepo(repo, path)
		}

		// Don't descend into the repo itself.
		return fs.SkipDir
	})
}

// addRepo records a discovered repo path. If there's a conflict, prefer the
// shorter path (likely the main clone rather than a worktree).
func (idx *Index) addRepo(repo, path string) {
	existing, ok := idx.repos[repo]
	if !ok || len(path) < len(existing) {
		idx.repos[repo] = path
	}
}

// depth returns how many directory levels path is below root.
// If path == root, depth is 0.
func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	if rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// repoFromGitConfig reads a .git/config file and extracts the GitHub org/repo
// from the [remote "origin"] URL.
func repoFromGitConfig(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	inOrigin := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Detect section headers.
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}

		if inOrigin && strings.HasPrefix(line, "url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return parseGitHubRepo(strings.TrimSpace(parts[1]))
			}
		}
	}
	return ""
}

// repoFromWorktreeGitFile reads a worktree .git file (e.g. "gitdir: /path/to/main/.git/worktrees/foo"),
// resolves back to the main repo's .git/config, and extracts the remote URL.
func repoFromWorktreeGitFile(dotGitFile string) string {
	data, err := os.ReadFile(dotGitFile)
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}

	gitdir := strings.TrimPrefix(line, "gitdir: ")

	// If the gitdir path is relative, resolve it against the directory containing .git file.
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(dotGitFile), gitdir)
	}

	// The gitdir typically looks like /path/to/main/.git/worktrees/<name>.
	// Walk up to find the main .git directory, then read its config.
	// Strategy: look for a "config" file by going up from gitdir until we find one.
	dir := filepath.Clean(gitdir)
	for {
		candidate := filepath.Join(dir, "config")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			// Verify this looks like a git config (has a [remote section or [core]).
			repo := repoFromGitConfig(candidate)
			if repo != "" {
				return repo
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// parseGitHubRepo extracts "org/repo" from a git remote URL.
// Returns empty string if the URL is not a GitHub remote.
//
// Supported formats:
//
//	git@github.com:org/repo.git
//	https://github.com/org/repo.git
//	https://github.com/org/repo
func parseGitHubRepo(remoteURL string) string {
	// SSH format: git@github.com:org/repo.git
	if path, ok := strings.CutPrefix(remoteURL, "git@github.com:"); ok {
		path = strings.TrimSuffix(path, ".git")
		if isValidOrgRepo(path) {
			return path
		}
		return ""
	}

	// HTTPS format: https://[user@]github.com/org/repo[.git]
	for _, scheme := range []string{"https://", "http://"} {
		if !strings.HasPrefix(remoteURL, scheme) {
			continue
		}
		rest := remoteURL[len(scheme):]
		// Strip optional user@ prefix (e.g. "starkmichelsc@github.com/...")
		if at := strings.Index(rest, "@"); at != -1 {
			host := rest[at+1:]
			if strings.HasPrefix(host, "github.com/") {
				rest = host
			}
		}
		if path, ok := strings.CutPrefix(rest, "github.com/"); ok {
			path = strings.TrimSuffix(path, ".git")
			parts := strings.SplitN(path, "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return parts[0] + "/" + parts[1]
			}
			return ""
		}
	}

	return ""
}

// isValidOrgRepo checks that a string looks like "org/repo" (exactly one slash, non-empty parts).
func isValidOrgRepo(s string) bool {
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
