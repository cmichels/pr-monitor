package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shurcooL/githubv4"
)

// FetchAuthoredPRs queries GitHub for open PRs authored by the viewer and
// detects recent review/comment activity by others. Retries transient errors up to 3 times.
func (p *Poller) FetchAuthoredPRs(ctx context.Context) ([]PollResult, error) {
	var results []PollResult
	err := retry(ctx, 3, func() error {
		var fetchErr error
		results, fetchErr = p.fetchAuthoredPRsOnce(ctx)
		return fetchErr
	})
	return results, err
}

func (p *Poller) fetchAuthoredPRsOnce(ctx context.Context) ([]PollResult, error) {
	query := fmt.Sprintf("is:open is:pr author:%s created:>%d-01-01%s", p.user, time.Now().Year(), p.repoExclusions())

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
						IsDraft      githubv4.Boolean
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
						TimelineItems struct {
							Nodes []struct {
								Typename          string `graphql:"__typename"`
								PullRequestReview struct {
									Author struct {
										Login githubv4.String
									}
									State     githubv4.PullRequestReviewState
									CreatedAt githubv4.DateTime
								} `graphql:"... on PullRequestReview"`
								IssueComment struct {
									Author struct {
										Login githubv4.String
									}
									CreatedAt githubv4.DateTime
								} `graphql:"... on IssueComment"`
							}
						} `graphql:"timelineItems(last: 5, itemTypes: [PULL_REQUEST_REVIEW, ISSUE_COMMENT])"`
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

			result := PollResult{
				PRID:         idStr,
				Repo:         string(pr.Repository.NameWithOwner),
				Number:       int(pr.Number),
				Title:        string(pr.Title),
				Author:       string(pr.Author.Login),
				URL:          pr.URL.String(),
				IsDraft:      bool(pr.IsDraft),
				FilesChanged: int(pr.ChangedFiles),
				CIStatus:     mapCIStatus(pr.Commits.Nodes),
				Role:         "author",
			}

			// Find the most recent non-self activity
			var latestTime time.Time
			for _, item := range pr.TimelineItems.Nodes {
				switch item.Typename {
				case "PullRequestReview":
					login := string(item.PullRequestReview.Author.Login)
					if login == p.user {
						continue
					}
					created := item.PullRequestReview.CreatedAt.Time
					if created.After(latestTime) {
						latestTime = created
						actType := mapReviewState(item.PullRequestReview.State)
						t := created
						result.LastActivityAt = &t
						result.LastActivityType = &actType
						result.LastActivityBy = &login
					}
				case "IssueComment":
					login := string(item.IssueComment.Author.Login)
					if login == p.user {
						continue
					}
					created := item.IssueComment.CreatedAt.Time
					if created.After(latestTime) {
						latestTime = created
						actType := "commented"
						t := created
						result.LastActivityAt = &t
						result.LastActivityType = &actType
						result.LastActivityBy = &login
					}
				}
			}

			results = append(results, result)
		}

		if !bool(q.Search.PageInfo.HasNextPage) {
			break
		}
		cursor = &q.Search.PageInfo.EndCursor
	}

	return results, nil
}

// mapReviewState converts a GitHub PullRequestReviewState to an internal string.
func mapReviewState(state githubv4.PullRequestReviewState) string {
	switch state {
	case githubv4.PullRequestReviewStateApproved:
		return "approved"
	case githubv4.PullRequestReviewStateChangesRequested:
		return "changes_requested"
	default:
		return "commented"
	}
}
