package detail

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Fetcher retrieves on-demand PR detail from GitHub's GraphQL API.
type Fetcher struct {
	client *githubv4.Client
}

// NewFetcher creates a Fetcher with the given GitHub token.
func NewFetcher(token string) *Fetcher {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(context.Background(), src)
	return &Fetcher{client: githubv4.NewClient(httpClient)}
}

// newFetcherWithClient is for testing — accepts an injected client.
func newFetcherWithClient(client *githubv4.Client) *Fetcher {
	return &Fetcher{client: client}
}

// newFetcherWithHTTPClient is for testing — creates a client from an http.Client.
func newFetcherWithHTTPClient(url string, httpClient *http.Client) *Fetcher {
	client := githubv4.NewEnterpriseClient(url+"/graphql", httpClient)
	return &Fetcher{client: client}
}

// reviewInput is a simplified representation of a review for computing per-reviewer state.
type reviewInput struct {
	Author string
	State  string
}

// requestInput is a simplified representation of a pending review request.
type requestInput struct {
	Name string
}

// FetchDetail queries GitHub for a PR's body, changed files, and CI checks.
func (f *Fetcher) FetchDetail(ctx context.Context, prNodeID string) (*PRDetail, error) {
	var q struct {
		Node struct {
			PullRequest struct {
				Body  githubv4.String
				Files struct {
					Nodes []struct {
						Path      githubv4.String
						Additions githubv4.Int
						Deletions githubv4.Int
					}
				} `graphql:"files(first: 100)"`
				Commits struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup struct {
								Contexts struct {
									Nodes []struct {
										Typename string `graphql:"__typename"`
										CheckRun struct {
											Name       githubv4.String
											Status     githubv4.CheckStatusState
											Conclusion githubv4.CheckConclusionState
										} `graphql:"... on CheckRun"`
										StatusContext struct {
											Context githubv4.String
											State   githubv4.StatusState
										} `graphql:"... on StatusContext"`
									}
								} `graphql:"contexts(first: 50)"`
							}
						}
					}
				} `graphql:"commits(last: 1)"`
				Comments struct {
					Nodes []struct {
						Author struct {
							Login githubv4.String
						}
						Body      githubv4.String
						CreatedAt githubv4.DateTime
					}
				} `graphql:"comments(last: 20)"`
				Reviews struct {
					Nodes []struct {
						Author struct {
							Login githubv4.String
						}
						Body      githubv4.String
						State     githubv4.PullRequestReviewState
						CreatedAt githubv4.DateTime
					}
				} `graphql:"reviews(last: 20)"`
				ReviewRequests struct {
					Nodes []struct {
						RequestedReviewer struct {
							User struct {
								Login githubv4.String
							} `graphql:"... on User"`
							Team struct {
								Name githubv4.String
							} `graphql:"... on Team"`
						}
					}
				} `graphql:"reviewRequests(first: 20)"`
			} `graphql:"... on PullRequest"`
		} `graphql:"node(id: $id)"`
	}

	vars := map[string]interface{}{
		"id": githubv4.ID(prNodeID),
	}

	if err := f.client.Query(ctx, &q, vars); err != nil {
		return nil, fmt.Errorf("fetch PR detail: %w", err)
	}

	pr := q.Node.PullRequest

	detail := &PRDetail{
		Body: string(pr.Body),
	}

	for _, file := range pr.Files.Nodes {
		detail.Files = append(detail.Files, FileChange{
			Path:      string(file.Path),
			Additions: int(file.Additions),
			Deletions: int(file.Deletions),
		})
	}

	if len(pr.Commits.Nodes) > 0 {
		for _, node := range pr.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts.Nodes {
			switch node.Typename {
			case "CheckRun":
				detail.Checks = append(detail.Checks, Check{
					Name:       string(node.CheckRun.Name),
					Status:     string(node.CheckRun.Status),
					Conclusion: string(node.CheckRun.Conclusion),
				})
			case "StatusContext":
				detail.Checks = append(detail.Checks, Check{
					Name:       string(node.StatusContext.Context),
					Status:     "COMPLETED",
					Conclusion: mapStatusState(node.StatusContext.State),
				})
			}
		}
	}

	// Merge PR comments and reviews into a single chronological list,
	// excluding bot accounts (e.g. swarmia).
	for _, c := range pr.Comments.Nodes {
		author := string(c.Author.Login)
		if isExcludedBot(author) {
			continue
		}
		detail.Comments = append(detail.Comments, Comment{
			Author:    author,
			Body:      string(c.Body),
			CreatedAt: c.CreatedAt.Time,
		})
	}
	for _, r := range pr.Reviews.Nodes {
		author := string(r.Author.Login)
		if isExcludedBot(author) {
			continue
		}
		body := string(r.Body)
		if body == "" {
			body = stateLabel(string(r.State))
		}
		detail.Comments = append(detail.Comments, Comment{
			Author:      author,
			Body:        body,
			CreatedAt:   r.CreatedAt.Time,
			ReviewState: string(r.State),
		})
	}
	sort.Slice(detail.Comments, func(i, j int) bool {
		return detail.Comments[i].CreatedAt.Before(detail.Comments[j].CreatedAt)
	})

	// Compute per-reviewer latest state for the Approvals summary.
	var reviews []reviewInput
	for _, r := range pr.Reviews.Nodes {
		reviews = append(reviews, reviewInput{
			Author: string(r.Author.Login),
			State:  string(r.State),
		})
	}
	var requests []requestInput
	for _, rr := range pr.ReviewRequests.Nodes {
		name := string(rr.RequestedReviewer.User.Login)
		if name == "" {
			name = string(rr.RequestedReviewer.Team.Name)
		}
		requests = append(requests, requestInput{Name: name})
	}
	detail.Reviews = computeReviewStatuses(reviews, requests)

	return detail, nil
}

// computeReviewStatuses computes the per-reviewer latest state.
// Reviews are walked in order (oldest to newest from GraphQL), keeping the last
// non-DISMISSED state per author. Unfulfilled reviewRequests are marked PENDING.
// Results are sorted: CHANGES_REQUESTED first, then PENDING, COMMENTED, APPROVED.
func computeReviewStatuses(reviews []reviewInput, requests []requestInput) []ReviewStatus {
	latest := make(map[string]string) // author/team -> state

	for _, r := range reviews {
		if r.Author == "" || isExcludedBot(r.Author) {
			continue
		}
		if r.State == "DISMISSED" {
			continue
		}
		latest[r.Author] = r.State
	}

	for _, rr := range requests {
		if rr.Name == "" {
			continue
		}
		if _, exists := latest[rr.Name]; !exists {
			latest[rr.Name] = "PENDING"
		}
	}

	result := make([]ReviewStatus, 0, len(latest))
	for author, state := range latest {
		result = append(result, ReviewStatus{Author: author, State: state})
	}

	sort.Slice(result, func(i, j int) bool {
		pi, pj := reviewSortPriority(result[i].State), reviewSortPriority(result[j].State)
		if pi != pj {
			return pi < pj
		}
		return result[i].Author < result[j].Author
	})

	return result
}

// reviewSortPriority returns a numeric priority for sorting review states.
// Lower = shown first. CHANGES_REQUESTED is most urgent.
func reviewSortPriority(state string) int {
	switch state {
	case "CHANGES_REQUESTED":
		return 0
	case "PENDING":
		return 1
	case "COMMENTED":
		return 2
	case "APPROVED":
		return 3
	default:
		return 4
	}
}

// isExcludedBot returns true if the author should be filtered from comments.
func isExcludedBot(login string) bool {
	lower := strings.ToLower(login)
	return strings.Contains(lower, "swarmia") || strings.HasSuffix(lower, "[bot]")
}

// stateLabel returns a human-readable label for a review state
// when the reviewer left no body text.
func stateLabel(state string) string {
	switch state {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes requested"
	case "COMMENTED":
		return "Reviewed"
	case "DISMISSED":
		return "Review dismissed"
	default:
		return "Reviewed"
	}
}

// mapStatusState converts a StatusState to a conclusion-like string.
func mapStatusState(state githubv4.StatusState) string {
	switch state {
	case githubv4.StatusStateSuccess:
		return "SUCCESS"
	case githubv4.StatusStateFailure, githubv4.StatusStateError:
		return "FAILURE"
	case githubv4.StatusStatePending:
		return "PENDING"
	default:
		return ""
	}
}
