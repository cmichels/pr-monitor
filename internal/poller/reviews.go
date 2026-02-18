package poller

import (
	"context"
	"fmt"

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

// FetchReviewRequests queries GitHub for PRs where the user or their teams
// are requested reviewers. Results are deduplicated by node ID and
// self-authored PRs are filtered out.
func (p *Poller) FetchReviewRequests(ctx context.Context) ([]PollResult, error) {
	// Build search queries: personal + one per team
	queries := []string{
		"is:open is:pr review-requested:@me",
	}
	for _, team := range p.teams {
		queries = append(queries, fmt.Sprintf("is:open is:pr team-review-requested:%s/%s", p.org, team))
	}

	seen := make(map[string]bool)
	var results []PollResult

	for _, q := range queries {
		prs, err := p.paginatedSearch(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", q, err)
		}
		for _, pr := range prs {
			// Deduplicate by node ID
			if seen[pr.PRID] {
				continue
			}
			// Filter out self-authored PRs
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
// collecting all pages of results.
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
		}

		vars := map[string]interface{}{
			"query":  githubv4.String(query),
			"cursor": cursor,
		}

		if err := p.client.Query(ctx, &q, vars); err != nil {
			return nil, err
		}

		for _, node := range q.Search.Nodes {
			pr := node.PullRequest
			idStr := fmt.Sprintf("%v", pr.ID)
			// Skip nodes with empty ID (non-PR results, if any)
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
