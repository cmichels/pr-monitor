# Ralph Phase 2: GitHub Poller

> **Goal**: Fetch real PR data from GitHub using GraphQL.
> **Depends on**: Phase 1.3 (auth)
> **Design Reference**: `plans/pr-monitor-final.md`

```json
[
  {
    "id": "2.1",
    "name": "Poll for review requests (personal + team)",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/poller/...",
    "max_iterations": 15
  },
  {
    "id": "2.2",
    "name": "Poll for authored PR activity",
    "test_command": "go build -o /dev/null ./... && go vet ./... && go test ./internal/poller/...",
    "max_iterations": 15
  }
]
```

---

## Task 2.1: Poll for Review Requests

Implement `internal/poller/reviews.go` — fetch PRs where the user or their
configured teams are requested reviewers via GitHub GraphQL.

### Context

- Target file: `internal/poller/reviews.go` (new file)
- Auth: `internal/poller/auth.go` provides `ResolveToken()`
- Config provides: `github.org`, `github.review_teams`
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Define the shared `PollResult` type in `internal/poller/types.go`:

```go
type PollResult struct {
    PRID         string // GitHub node ID
    Repo         string // "org/repo"
    Number       int
    Title        string
    Author       string
    URL          string
    FilesChanged int
    CIStatus     string // "passing", "failing", "pending", "unknown"
    Role         string // "reviewer" or "author"

    // Only for authored PRs (Role == "author")
    LastActivityAt   *time.Time
    LastActivityType *string
    LastActivityBy   *string
}
```

2. Implement the Poller struct and review request fetching in `internal/poller/reviews.go`:

```go
type Poller struct {
    client *githubv4.Client
    org    string
    teams  []string
    user   string // resolved from GitHub API (viewer login)
}

func NewPoller(token string, org string, teams []string) (*Poller, error)
func (p *Poller) FetchReviewRequests(ctx context.Context) ([]PollResult, error)
```

3. GraphQL approach — use GitHub's search API:
- Query 1: `is:open is:pr review-requested:@me -author:app/dependabot created:>2026-01-01` (personal requests, excluding Dependabot, only PRs after Jan 1 2026)
- Query 2+: `is:open is:pr team-review-requested:{org}/{team} -author:app/dependabot created:>2026-01-01` for each team
- Combine results and deduplicate by PR node ID
- **IMPORTANT: Filter out self-authored PRs** — if `pr.author.login == p.user` (the viewer), drop the PR from review results. The viewer cannot review their own PR (standard Git rules). The authored PR pipeline will pick it up instead.
- For each PR, extract: node ID, repository nameWithOwner, number, title, author login, URL, changedFiles count
- For CI status: check `commits(last:1) { nodes { commit { statusCheckRollup { state } } } }`
  - Map: SUCCESS → "passing", FAILURE/ERROR → "failing", PENDING → "pending", null → "unknown"

4. Resolve the viewer's login on `NewPoller`:
```graphql
query { viewer { login } }
```

5. Handle pagination: use `pageInfo { hasNextPage, endCursor }` with `after:` parameter. Most users won't have >100 review requests, but handle it.

6. Write tests in `internal/poller/reviews_test.go`:
- Mock the GraphQL client or use a test HTTP server that returns canned responses
- Test single review request parsing
- Test deduplication (same PR from personal + team request)
- Test self-authored PRs are filtered out of review results
- Test CI status mapping (all states)
- Test empty results
- Test pagination handling

### Success Criteria

1. [ ] `internal/poller/types.go` defines `PollResult` struct
2. [ ] `internal/poller/reviews.go` implements `Poller` with `NewPoller` and `FetchReviewRequests`
3. [ ] Queries for personal + all configured team review requests
4. [ ] Results deduplicated by node ID
4a. [ ] Self-authored PRs filtered out of review results
5. [ ] CI status correctly mapped from statusCheckRollup
6. [ ] Viewer login resolved on construction
7. [ ] `internal/poller/reviews_test.go` covers parsing, dedup, CI mapping, empty, pagination
8. [ ] `go build -o /dev/null ./...` succeeds
9. [ ] `go vet ./...` is clean
10. [ ] `go test ./internal/poller/...` — all tests pass

### Constraints

- Use `github.com/shurcooL/githubv4` for GraphQL — do NOT use REST API
- Create the githubv4 client with `oauth2.StaticTokenSource` + `oauth2.NewClient` + `githubv4.NewClient`
- Do NOT import any other internal/ packages (config, store, etc.)
- Tests must NOT make real GitHub API calls
- Set `Role: "reviewer"` on all results from this function

### Delegation Strategy

- **Delegate to subagents**: Read `githubv4` docs for search query syntax, pagination patterns, and struct tag conventions
- **Keep in main context**: Write reviews.go, types.go, and tests

### Output

When all criteria are met, output: DONE

---

## Task 2.2: Poll for Authored PR Activity

Implement `internal/poller/authored.go` — fetch open PRs authored by the user
and detect recent review/comment activity.

### Context

- Target file: `internal/poller/authored.go` (new file)
- Shared types: `internal/poller/types.go` (PollResult from Task 2.1)
- Poller struct: `internal/poller/reviews.go` (from Task 2.1)
- Guardrails: `.ralph/guardrails.md`

### What to Do

1. Add method to the existing Poller struct:

```go
func (p *Poller) FetchAuthoredPRs(ctx context.Context) ([]PollResult, error)
```

2. GraphQL approach:
- Search: `is:open is:pr author:{viewer.login} created:>2026-01-01` (Dependabot filter not needed here since viewer is never dependabot)
- For each PR, fetch `timelineItems(last: 5, itemTypes: [PULL_REQUEST_REVIEW, ISSUE_COMMENT])`:
  - For `PULL_REQUEST_REVIEW`: extract `author.login`, `state` (APPROVED, CHANGES_REQUESTED, COMMENTED), `createdAt`
  - For `ISSUE_COMMENT`: extract `author.login`, `createdAt`
- Determine the most recent activity that is NOT by the viewer (ignore self-comments)
- Map to PollResult with:
  - `Role: "author"`
  - `LastActivityAt`: timestamp of most recent non-self activity
  - `LastActivityType`: "approved", "changes_requested", or "commented"
  - `LastActivityBy`: login of who performed the action
  - If no non-self activity exists, still return the PR but with nil activity fields

3. Write tests in `internal/poller/authored_test.go`:
- Test parsing of authored PR with approval activity
- Test parsing with comment activity
- Test parsing with changes_requested activity
- Test self-activity is filtered out (only non-self activity reported)
- Test PR with no activity (nil activity fields)
- Test multiple activities — most recent wins

### Success Criteria

1. [ ] `internal/poller/authored.go` implements `FetchAuthoredPRs`
2. [ ] Fetches timeline items for review and comment activity
3. [ ] Filters out self-activity (viewer's own comments/reviews)
4. [ ] Maps GitHub review states to internal types (approved/changes_requested/commented)
5. [ ] Returns most recent non-self activity per PR
6. [ ] `internal/poller/authored_test.go` covers all activity types and edge cases
7. [ ] `go build -o /dev/null ./...` succeeds
8. [ ] `go vet ./...` is clean
9. [ ] `go test ./internal/poller/...` — all tests pass

### Constraints

- Add to the existing Poller struct — do NOT create a separate struct
- Use `github.com/shurcooL/githubv4` — same client as reviews.go
- Do NOT import any other internal/ packages
- Tests must NOT make real GitHub API calls
- Set `Role: "author"` on all results from this function
- Ignore the viewer's own reviews/comments when determining "latest activity"

### Delegation Strategy

- **Delegate to subagents**: Read githubv4 docs for `timelineItems` connection, review state enum
- **Keep in main context**: Write authored.go and tests

### Output

When all criteria are met, output: DONE
