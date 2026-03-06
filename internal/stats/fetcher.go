package stats

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"

	"github.com/chrismichels/pr-monitor/internal/store"
)

// Fetcher retrieves contributor statistics from GitHub's GraphQL API.
type Fetcher struct {
	client *githubv4.Client
	org    string
	year   int
}

// NewFetcher creates a Fetcher with the given GitHub token and organization.
func NewFetcher(token, org string) *Fetcher {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(context.Background(), src)
	return &Fetcher{
		client: githubv4.NewClient(httpClient),
		org:    org,
		year:   time.Now().Year(),
	}
}

// newFetcherWithHTTPClient is for testing with a mock HTTP server.
func newFetcherWithHTTPClient(url string, httpClient *http.Client, org string) *Fetcher {
	client := githubv4.NewEnterpriseClient(url+"/graphql", httpClient)
	return &Fetcher{
		client: client,
		org:    org,
		year:   2026,
	}
}

// FetchTeamMembers fetches members of all given team slugs, deduplicating logins.
func (f *Fetcher) FetchTeamMembers(ctx context.Context, teams []string) ([]string, error) {
	seen := make(map[string]bool)
	var result []string

	for _, team := range teams {
		members, err := f.fetchTeamMembersPage(ctx, team)
		if err != nil {
			return nil, fmt.Errorf("fetch team %s members: %w", team, err)
		}
		for _, login := range members {
			if !seen[login] {
				seen[login] = true
				result = append(result, login)
			}
		}
	}

	return result, nil
}

// fetchTeamMembersPage fetches all members of a single team with pagination.
func (f *Fetcher) fetchTeamMembersPage(ctx context.Context, teamSlug string) ([]string, error) {
	var q struct {
		Organization struct {
			Team struct {
				Members struct {
					Nodes []struct {
						Login githubv4.String
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   githubv4.String
					}
				} `graphql:"members(first: 100, after: $cursor)"`
			} `graphql:"team(slug: $team)"`
		} `graphql:"organization(login: $org)"`
		RateLimit struct {
			Remaining githubv4.Int
			ResetAt   githubv4.DateTime
		}
	}

	var members []string
	var cursor *githubv4.String

	for {
		vars := map[string]interface{}{
			"org":    githubv4.String(f.org),
			"team":   githubv4.String(teamSlug),
			"cursor": cursor,
		}

		if err := f.client.Query(ctx, &q, vars); err != nil {
			return nil, err
		}

		for _, node := range q.Organization.Team.Members.Nodes {
			members = append(members, string(node.Login))
		}

		if err := f.checkRateLimit(ctx, int(q.RateLimit.Remaining), q.RateLimit.ResetAt.Time); err != nil {
			return nil, err
		}

		if !q.Organization.Team.Members.PageInfo.HasNextPage {
			break
		}
		cursor = &q.Organization.Team.Members.PageInfo.EndCursor
	}

	return members, nil
}

// FetchUserStats fetches all contribution stats for a user in the configured year.
// Returns a map of date string -> DailyStat.
func (f *Fetcher) FetchUserStats(ctx context.Context, login string) (map[string]store.DailyStat, error) {
	result := make(map[string]store.DailyStat)

	// Query A: Authored PRs.
	if err := f.fetchAuthoredPRs(ctx, login, result); err != nil {
		return nil, fmt.Errorf("fetch authored PRs for %s: %w", login, err)
	}

	// Query B: Reviews given on others' PRs.
	if err := f.fetchReviewsGiven(ctx, login, result); err != nil {
		return nil, fmt.Errorf("fetch reviews for %s: %w", login, err)
	}

	return result, nil
}

// fetchAuthoredPRs queries for PRs created by the user in the configured year.
func (f *Fetcher) fetchAuthoredPRs(ctx context.Context, login string, result map[string]store.DailyStat) error {
	var q struct {
		Search struct {
			Nodes []struct {
				PullRequest struct {
					CreatedAt githubv4.DateTime
					MergedAt  *githubv4.DateTime
					Additions githubv4.Int
					Deletions githubv4.Int
				} `graphql:"... on PullRequest"`
			}
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
		} `graphql:"search(query: $query, type: ISSUE, first: 100, after: $cursor)"`
		RateLimit struct {
			Remaining githubv4.Int
			ResetAt   githubv4.DateTime
		}
	}

	query := fmt.Sprintf("is:pr author:%s created:%d-01-01..%d-12-31", login, f.year, f.year)
	var cursor *githubv4.String

	for {
		vars := map[string]interface{}{
			"query":  githubv4.String(query),
			"cursor": cursor,
		}

		if err := f.client.Query(ctx, &q, vars); err != nil {
			return err
		}

		for _, node := range q.Search.Nodes {
			pr := node.PullRequest
			createdDate := pr.CreatedAt.Format("2006-01-02")

			stat := result[createdDate]
			stat.Login = login
			stat.Date = createdDate
			stat.PRsCreated++
			stat.LinesAdded += int(pr.Additions)
			stat.LinesRemoved += int(pr.Deletions)
			result[createdDate] = stat

			if pr.MergedAt != nil {
				mergedDate := pr.MergedAt.Format("2006-01-02")
				mStat := result[mergedDate]
				mStat.Login = login
				mStat.Date = mergedDate
				mStat.PRsMerged++
				result[mergedDate] = mStat
			}
		}

		if err := f.checkRateLimit(ctx, int(q.RateLimit.Remaining), q.RateLimit.ResetAt.Time); err != nil {
			return err
		}

		if !q.Search.PageInfo.HasNextPage {
			break
		}
		cursor = &q.Search.PageInfo.EndCursor
	}

	return nil
}

// fetchReviewsGiven queries for reviews the user gave on others' PRs.
func (f *Fetcher) fetchReviewsGiven(ctx context.Context, login string, result map[string]store.DailyStat) error {
	var q struct {
		Search struct {
			Nodes []struct {
				PullRequest struct {
					ID      githubv4.ID
					Reviews struct {
						Nodes []struct {
							Author struct {
								Login githubv4.String
							}
							State       githubv4.PullRequestReviewState
							SubmittedAt githubv4.DateTime
							Comments    struct {
								TotalCount githubv4.Int
							} `graphql:"comments"`
						}
					} `graphql:"reviews(first: 100)"`
					Comments struct {
						Nodes []struct {
							Author struct {
								Login githubv4.String
							}
							CreatedAt githubv4.DateTime
						}
					} `graphql:"comments(first: 100)"`
				} `graphql:"... on PullRequest"`
			}
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
		} `graphql:"search(query: $query, type: ISSUE, first: 50, after: $cursor)"`
		RateLimit struct {
			Remaining githubv4.Int
			ResetAt   githubv4.DateTime
		}
	}

	query := fmt.Sprintf("is:pr reviewed-by:%s -author:%s created:>=%d-01-01", login, login, f.year)
	var cursor *githubv4.String

	// Track unique PR IDs per date to count prs_reviewed correctly.
	reviewedPRs := make(map[string]map[string]bool) // date -> set of PR IDs

	for {
		vars := map[string]interface{}{
			"query":  githubv4.String(query),
			"cursor": cursor,
		}

		if err := f.client.Query(ctx, &q, vars); err != nil {
			return err
		}

		for _, node := range q.Search.Nodes {
			pr := node.PullRequest
			prID := fmt.Sprintf("%v", pr.ID)

			// Process reviews by this user.
			for _, review := range pr.Reviews.Nodes {
				if string(review.Author.Login) != login {
					continue
				}
				date := review.SubmittedAt.Format("2006-01-02")

				stat := result[date]
				stat.Login = login
				stat.Date = date

				// Count unique PR reviews per day.
				if reviewedPRs[date] == nil {
					reviewedPRs[date] = make(map[string]bool)
				}
				if !reviewedPRs[date][prID] {
					reviewedPRs[date][prID] = true
					stat.PRsReviewed++
				}

				switch review.State {
				case "APPROVED":
					stat.ApprovalsGiven++
				case "CHANGES_REQUESTED":
					stat.ChangesRequested++
				}

				// Count inline code review comments from this review.
				stat.CommentsGiven += int(review.Comments.TotalCount)

				result[date] = stat
			}

			// Process issue comments (top-level PR conversation) by this user.
			for _, comment := range pr.Comments.Nodes {
				if string(comment.Author.Login) != login {
					continue
				}
				date := comment.CreatedAt.Format("2006-01-02")

				stat := result[date]
				stat.Login = login
				stat.Date = date
				stat.CommentsGiven++
				result[date] = stat
			}
		}

		if err := f.checkRateLimit(ctx, int(q.RateLimit.Remaining), q.RateLimit.ResetAt.Time); err != nil {
			return err
		}

		if !q.Search.PageInfo.HasNextPage {
			break
		}
		cursor = &q.Search.PageInfo.EndCursor
	}

	return nil
}

// checkRateLimit sleeps if remaining API calls are below a safe threshold.
func (f *Fetcher) checkRateLimit(ctx context.Context, remaining int, resetAt time.Time) error {
	if remaining >= 50 {
		return nil
	}
	wait := time.Until(resetAt) + 5*time.Second
	if wait <= 0 {
		return nil
	}
	slog.Warn("rate limit low, waiting", "remaining", remaining, "wait", wait.Round(time.Second))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}
