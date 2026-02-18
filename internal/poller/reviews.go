package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Poller fetches PR data from GitHub's GraphQL API.
type Poller struct {
	client *githubv4.Client
	org    string
	teams  []string
	user   string // viewer login resolved from GitHub API
}

// NewPoller creates a Poller and resolves the viewer's GitHub login.
func NewPoller(token string, org string, teams []string) (*Poller, error) {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(context.Background(), src)
	client := githubv4.NewClient(httpClient)

	return newPollerWithClient(client, org, teams)
}

// newPollerWithClient is the internal constructor that accepts an injected client (for testing).
func newPollerWithClient(client *githubv4.Client, org string, teams []string) (*Poller, error) {
	var vq struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(context.Background(), &vq, nil); err != nil {
		return nil, fmt.Errorf("failed to resolve viewer login: %w", err)
	}

	return &Poller{
		client: client,
		org:    org,
		teams:  teams,
		user:   string(vq.Viewer.Login),
	}, nil
}

// checkRateLimit inspects the rate limit response and returns a recommended
// sleep duration. Logs warnings when remaining is low.
func (p *Poller) checkRateLimit(remaining int, resetAt time.Time) time.Duration {
	if remaining < 10 {
		wait := time.Until(resetAt) + 10*time.Second // buffer
		if wait < 0 {
			wait = 0
		}
		slog.Warn("GitHub rate limit low",
			"remaining", remaining,
			"reset_at", resetAt.Format(time.RFC3339),
			"wait", wait,
		)
		return wait
	}
	return 0
}

// classifyError inspects a GraphQL client error and wraps it with a typed error
// if it indicates an auth failure (401) or rate limit (403).
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized") {
		return &AuthExpiredError{Msg: "GitHub token expired. Run: gh auth login"}
	}
	if strings.Contains(msg, "403") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "API rate limit") {
		return &RateLimitError{ResetAt: time.Now().Add(5 * time.Minute), Remaining: 0}
	}
	return err
}

// FetchReviewRequests queries GitHub for PRs where the user or their teams
// are requested reviewers. Results are deduplicated by node ID and
// self-authored PRs are filtered out. Retries transient errors up to 3 times.
func (p *Poller) FetchReviewRequests(ctx context.Context) ([]PollResult, error) {
	// Build search queries: personal + one per team
	queries := []string{
		"is:open is:pr -is:draft review-requested:@me -author:app/dependabot created:>2026-01-01",
	}
	for _, team := range p.teams {
		queries = append(queries, fmt.Sprintf("is:open is:pr -is:draft team-review-requested:%s/%s -author:app/dependabot created:>2026-01-01", p.org, team))
	}

	seen := make(map[string]bool)
	var results []PollResult

	for _, q := range queries {
		var prs []PollResult
		err := retry(ctx, 3, func() error {
			var fetchErr error
			prs, fetchErr = p.paginatedSearch(ctx, q)
			return fetchErr
		})
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", q, err)
		}
		for _, pr := range prs {
			if seen[pr.PRID] {
				continue
			}
			if pr.Author == p.user {
				continue
			}
			seen[pr.PRID] = true
			results = append(results, pr)
		}
	}

	return results, nil
}

// paginatedSearch runs a single search query with cursor-based pagination,
// collecting all pages of results. Checks rate limit after each page.
func (p *Poller) paginatedSearch(ctx context.Context, query string) ([]PollResult, error) {
	var results []PollResult
	var cursor *githubv4.String

	for {
		var q struct {
			Search struct {
				PageInfo struct {
					HasNextPage githubv4.Boolean
					EndCursor   githubv4.String
				}
				Nodes []struct {
					PullRequest struct {
						ID         githubv4.ID
						Number     githubv4.Int
						Title      githubv4.String
						URL        githubv4.URI
						Repository struct {
							NameWithOwner githubv4.String
						}
						Author struct {
							Login githubv4.String
						}
						ChangedFiles githubv4.Int
						Commits      struct {
							Nodes []struct {
								Commit struct {
									StatusCheckRollup struct {
										State githubv4.StatusState
									}
								}
							}
						} `graphql:"commits(last: 1)"`
					} `graphql:"... on PullRequest"`
				}
			} `graphql:"search(query: $query, type: ISSUE, first: 100, after: $cursor)"`
			RateLimit struct {
				Remaining githubv4.Int
				ResetAt   githubv4.DateTime
			}
		}

		vars := map[string]interface{}{
			"query":  githubv4.String(query),
			"cursor": cursor,
		}

		if err := p.client.Query(ctx, &q, vars); err != nil {
			return nil, classifyError(err)
		}

		// Check rate limit after each page.
		remaining := int(q.RateLimit.Remaining)
		resetAt := q.RateLimit.ResetAt.Time
		if wait := p.checkRateLimit(remaining, resetAt); wait > 0 {
			if remaining == 0 {
				return nil, &RateLimitError{ResetAt: resetAt, Remaining: remaining}
			}
			slog.Info("rate limit low, pausing before next page", "wait", wait)
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(wait):
			}
		}

		for _, node := range q.Search.Nodes {
			pr := node.PullRequest
			idStr := fmt.Sprintf("%v", pr.ID)
			if idStr == "" || idStr == "<nil>" {
				continue
			}
			results = append(results, PollResult{
				PRID:         idStr,
				Repo:         string(pr.Repository.NameWithOwner),
				Number:       int(pr.Number),
				Title:        string(pr.Title),
				Author:       string(pr.Author.Login),
				URL:          pr.URL.String(),
				FilesChanged: int(pr.ChangedFiles),
				CIStatus:     mapCIStatus(pr.Commits.Nodes),
				Role:         "reviewer",
			})
		}

		if !bool(q.Search.PageInfo.HasNextPage) {
			break
		}
		cursor = &q.Search.PageInfo.EndCursor
	}

	return results, nil
}

// mapCIStatus converts the statusCheckRollup state to a human-readable string.
func mapCIStatus(commits []struct {
	Commit struct {
		StatusCheckRollup struct {
			State githubv4.StatusState
		}
	}
}) string {
	if len(commits) == 0 {
		return "unknown"
	}
	switch commits[0].Commit.StatusCheckRollup.State {
	case githubv4.StatusStateSuccess:
		return "passing"
	case githubv4.StatusStateFailure, githubv4.StatusStateError:
		return "failing"
	case githubv4.StatusStatePending:
		return "pending"
	default:
		return "unknown"
	}
}
