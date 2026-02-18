# Ralph Phase 4: Repo Discovery

> **Goal**: Scan the local filesystem for git repos and map them to GitHub remotes.
> **Depends on**: Phase 1.1 (config for workspace_dirs)
> **Design Reference**: `plans/pr-monitor-final.md`

```json
[
  {
    "id": "4.1",
    "name": "Filesystem repo scanner with worktree support",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/discover/...",
    "max_iterations": 15
  }
]
```

---

## Task 4.1: Filesystem Repo Scanner with Worktree Support

Implement `internal/discover/discover.go` — scan workspace directories for
git repositories, parse their remotes, build a mapping of GitHub repo names
to local filesystem paths. Handle both regular clones and git worktrees.

### Context

- Target file: `internal/discover/discover.go` (exists, currently just `package discover`)
- Config provides `workspace_dirs` (directories to scan) and `repo_overrides` (explicit mappings)
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Implement `internal/discover/discover.go`:

```go
// Index maps GitHub repository names to local filesystem paths.
type Index struct {
    repos     map[string]string // "org/repo" → "/local/path"
    overrides map[string]string // from config, takes precedence
}

func NewIndex(overrides map[string]string) *Index

// Scan walks the given directories looking for git repos and builds the index.
func (idx *Index) Scan(dirs []string) error

// Resolve returns the local path for a GitHub repo, or false if not found.
func (idx *Index) Resolve(repo string) (string, bool)

// Repos returns all known repo → path mappings (for display/debug).
func (idx *Index) Repos() map[string]string
```

2. Scanning logic:

Walk each directory in `dirs` looking for entries that contain a `.git` indicator.
Do NOT recurse infinitely — limit depth to 4 levels (most repos are 1-2 levels deep
under a workspace dir like `/Volumes/data/projects/org/repo`).

For each directory with a `.git` entry:

**Case A — Regular clone (`.git` is a directory)**:
- Read `.git/config` file
- Parse the `[remote "origin"]` section for the `url` line
- Extract GitHub org/repo from the URL

**Case B — Worktree (`.git` is a file)**:
- Read the `.git` file content: `gitdir: /path/to/main-repo/.git/worktrees/name`
- Follow the path to the main repo's `.git/config`
- Parse the remote URL from there

3. GitHub URL parsing — handle these formats:
- SSH: `git@github.com:org/repo.git` → `org/repo`
- HTTPS: `https://github.com/org/repo.git` → `org/repo`
- HTTPS without .git: `https://github.com/org/repo` → `org/repo`
- Strip trailing `.git` suffix

```go
// parseGitHubRepo extracts "org/repo" from a git remote URL.
// Returns empty string if the URL is not a GitHub remote.
func parseGitHubRepo(remoteURL string) string
```

4. Override precedence:
- Config `repo_overrides` always win over discovered paths
- If multiple local paths exist for the same repo (e.g., main clone + worktree),
  prefer the one with the shorter path (likely the main clone)

5. Write tests in `internal/discover/discover_test.go`:
- Create temp directory trees with mock `.git/config` files
- Test regular clone discovery (SSH URL)
- Test regular clone discovery (HTTPS URL)
- Test worktree discovery (`.git` file pointing to parent)
- Test URL parsing for all formats
- Test override precedence
- Test Resolve returns correct path
- Test Resolve returns false for unknown repo
- Test depth limiting (don't descend too deep)

### Success Criteria

1. [ ] `internal/discover/discover.go` implements Index with Scan, Resolve, Repos
2. [ ] Handles regular clones (`.git/` directory with config)
3. [ ] Handles worktrees (`.git` file → resolve parent repo remote)
4. [ ] Parses SSH and HTTPS GitHub URLs correctly
5. [ ] Config overrides take precedence over discovered paths
6. [ ] Scan depth is limited (not infinite recursion)
7. [ ] `internal/discover/discover_test.go` covers all cases with temp directories
8. [ ] `go build -o /dev/null ./...` succeeds
9. [ ] `go vet ./...` is clean
10. [ ] `go test ./internal/discover/...` — all tests pass

### Constraints

- Do NOT import any other internal/ packages
- Use `os` and `filepath` for filesystem operations — no third-party filesystem libraries
- Parse git config manually (simple line parsing) — do NOT import a git library for this
- The `.git/config` format is INI-like. Only need to find `[remote "origin"]` section and `url = ...` line
- Limit scan depth to 4 levels to avoid scanning into node_modules, vendor, etc.
- Use `os.Stat` to check if `.git` is a file or directory
- For worktrees, the `.git` file content format is: `gitdir: <path>\n`

### Delegation Strategy

- **Delegate to subagents**: Read git documentation for worktree `.git` file format, `.git/config` format
- **Keep in main context**: Write discover.go and tests

### Output

When all criteria are met, output: DONE
