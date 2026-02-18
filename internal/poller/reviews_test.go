package poller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphQLRequest is the shape of a shurcooL/githubv4 HTTP request body.
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// newTestServer creates an httptest.Server that dispatches responses based on the
// GraphQL query content. The handler func receives the parsed request and returns
// the JSON response data (the "data" field will be wrapped automatically).
func newTestServer(t *testing.T, handler func(req graphQLRequest) interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		data := handler(req)
		resp := map[string]interface{}{"data": data}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

// newTestPoller creates a Poller backed by the given test server URL.
// The server must handle the viewer query that NewPoller sends.
func newTestPoller(t *testing.T, serverURL string, org string, teams []string, viewerLogin string) *Poller {
	t.Helper()
	// Bypass NewPoller (which calls the real GitHub API) and directly construct
	// a Poller with a client pointed at our test server.
	client := githubv4.NewEnterpriseClient(serverURL+"/graphql", http.DefaultClient)
	return &Poller{
		client: client,
		org:    org,
		teams:  teams,
		user:   viewerLogin,
	}
}

// searchResultPR builds a single PR node for a search result JSON response.
func searchResultPR(id, repo string, number int, title, author, url string, files int, ciState string) map[string]interface{} {
	commits := []interface{}{
		map[string]interface{}{
			"commit": map[string]interface{}{
				"statusCheckRollup": map[string]interface{}{
					"state": ciState,
				},
			},
		},
	}
	// Empty state means no status check rollup
	if ciState == "" {
		commits = []interface{}{
			map[string]interface{}{
				"commit": map[string]interface{}{
					"statusCheckRollup": map[string]interface{}{},
				},
			},
		}
	}

	return map[string]interface{}{
		"id": id,
		"number":     number,
		"title":      title,
		"url":        url,
		"repository": map[string]interface{}{
			"nameWithOwner": repo,
		},
		"author": map[string]interface{}{
			"login": author,
		},
		"changedFiles": files,
		"commits": map[string]interface{}{
			"nodes": commits,
		},
	}
}

// searchResponse builds a complete search query response with pagination.
func searchResponse(prs []map[string]interface{}, hasNextPage bool, endCursor string) map[string]interface{} {
	return map[string]interface{}{
		"search": map[string]interface{}{
			"nodes": prs,
			"pageInfo": map[string]interface{}{
				"hasNextPage": hasNextPage,
				"endCursor":   endCursor,
			},
		},
	}
}

func TestNewPoller_ResolvesViewerLogin(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		// The viewer query
		if strings.Contains(req.Query, "viewer") {
			return map[string]interface{}{
				"viewer": map[string]interface{}{
					"login": "testuser",
				},
			}
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	client := githubv4.NewEnterpriseClient(srv.URL+"/graphql", http.DefaultClient)
	p, err := newPollerWithClient(client, "myorg", []string{"team1"})
	require.NoError(t, err)
	assert.Equal(t, "testuser", p.user)
	assert.Equal(t, "myorg", p.org)
	assert.Equal(t, []string{"team1"}, p.teams)
}

func TestNewPoller_ViewerQueryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{
				{"message": "Bad credentials"},
			},
		})
	}))
	defer srv.Close()

	client := githubv4.NewEnterpriseClient(srv.URL+"/graphql", http.DefaultClient)
	_, err := newPollerWithClient(client, "myorg", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve viewer login")
}

func TestFetchReviewRequests_SinglePR(t *testing.T) {
	callCount := 0
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		callCount++
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				searchResultPR("PR_1", "myorg/myrepo", 42, "Fix bug", "alice", "https://github.com/myorg/myrepo/pull/42", 3, "SUCCESS"),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "PR_1", results[0].PRID)
	assert.Equal(t, "myorg/myrepo", results[0].Repo)
	assert.Equal(t, 42, results[0].Number)
	assert.Equal(t, "Fix bug", results[0].Title)
	assert.Equal(t, "alice", results[0].Author)
	assert.Equal(t, "https://github.com/myorg/myrepo/pull/42", results[0].URL)
	assert.Equal(t, 3, results[0].FilesChanged)
	assert.Equal(t, "passing", results[0].CIStatus)
	assert.Equal(t, "reviewer", results[0].Role)
}

func TestFetchReviewRequests_Deduplication(t *testing.T) {
	// Same PR appears in both personal and team search results
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			// Return the same PR for every search query
			return searchResponse([]map[string]interface{}{
				searchResultPR("PR_DUP", "myorg/shared", 10, "Shared PR", "bob", "https://github.com/myorg/shared/pull/10", 5, "SUCCESS"),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	// Two teams means 3 queries total (personal + 2 teams)
	p := newTestPoller(t, srv.URL, "myorg", []string{"team-a", "team-b"}, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	// Despite 3 queries returning the same PR, we should only get 1 result
	require.Len(t, results, 1)
	assert.Equal(t, "PR_DUP", results[0].PRID)
}

func TestFetchReviewRequests_SelfAuthoredFiltered(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				searchResultPR("PR_MINE", "myorg/repo", 1, "My PR", "testuser", "https://github.com/myorg/repo/pull/1", 2, "SUCCESS"),
				searchResultPR("PR_OTHER", "myorg/repo", 2, "Their PR", "alice", "https://github.com/myorg/repo/pull/2", 4, "PENDING"),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	// Self-authored PR should be filtered out
	require.Len(t, results, 1)
	assert.Equal(t, "PR_OTHER", results[0].PRID)
	assert.Equal(t, "alice", results[0].Author)
}

func TestFetchReviewRequests_CIStatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		ciState  string
		expected string
	}{
		{"success", "SUCCESS", "passing"},
		{"failure", "FAILURE", "failing"},
		{"error", "ERROR", "failing"},
		{"pending", "PENDING", "pending"},
		{"empty/null", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(req graphQLRequest) interface{} {
				if strings.Contains(req.Query, "search") {
					return searchResponse([]map[string]interface{}{
						searchResultPR("PR_CI", "myorg/repo", 1, "CI Test", "alice", "https://github.com/myorg/repo/pull/1", 1, tt.ciState),
					}, false, "")
				}
				t.Fatalf("unexpected query: %s", req.Query)
				return nil
			})
			defer srv.Close()

			p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
			results, err := p.FetchReviewRequests(context.Background())
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, tt.expected, results[0].CIStatus)
		})
	}
}

func TestFetchReviewRequests_NoCommits(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			pr := searchResultPR("PR_NOCOMMIT", "myorg/repo", 1, "Empty", "alice", "https://github.com/myorg/repo/pull/1", 0, "SUCCESS")
			// Override commits to be empty
			pr["commits"] = map[string]interface{}{
				"nodes": []interface{}{},
			}
			return searchResponse([]map[string]interface{}{pr}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "unknown", results[0].CIStatus)
}

func TestFetchReviewRequests_EmptyResults(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse(nil, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", []string{"team1"}, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestFetchReviewRequests_Pagination(t *testing.T) {
	page := 0
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			page++
			switch page {
			case 1:
				// First page: has next page
				return searchResponse([]map[string]interface{}{
					searchResultPR("PR_PAGE1", "myorg/repo", 1, "Page 1 PR", "alice", "https://github.com/myorg/repo/pull/1", 2, "SUCCESS"),
				}, true, "cursor_abc")
			case 2:
				// Second page: no more pages
				return searchResponse([]map[string]interface{}{
					searchResultPR("PR_PAGE2", "myorg/repo", 2, "Page 2 PR", "bob", "https://github.com/myorg/repo/pull/2", 3, "FAILURE"),
				}, false, "")
			default:
				t.Fatalf("unexpected page %d", page)
			}
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	// No teams — just personal query, so only 1 search query with 2 pages
	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 2)
	assert.Equal(t, "PR_PAGE1", results[0].PRID)
	assert.Equal(t, "passing", results[0].CIStatus)
	assert.Equal(t, "PR_PAGE2", results[1].PRID)
	assert.Equal(t, "failing", results[1].CIStatus)
}

func TestFetchReviewRequests_MultipleTeams(t *testing.T) {
	queryCount := 0
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			queryCount++
			// Return a unique PR for each query
			id := "PR_Q" + string(rune('0'+queryCount))
			return searchResponse([]map[string]interface{}{
				searchResultPR(id, "myorg/repo", queryCount, "PR "+id, "alice", "https://github.com/myorg/repo/pull/"+string(rune('0'+queryCount)), 1, "SUCCESS"),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", []string{"team-a", "team-b"}, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	// 3 queries (personal + 2 teams), each returns 1 unique PR
	assert.Len(t, results, 3)
	assert.Equal(t, 3, queryCount)
}

func TestFetchReviewRequests_AllRolesSetToReviewer(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				searchResultPR("PR_A", "myorg/repo", 1, "PR A", "alice", "https://github.com/myorg/repo/pull/1", 1, "SUCCESS"),
				searchResultPR("PR_B", "myorg/repo", 2, "PR B", "bob", "https://github.com/myorg/repo/pull/2", 2, "PENDING"),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchReviewRequests(context.Background())
	require.NoError(t, err)

	for _, r := range results {
		assert.Equal(t, "reviewer", r.Role, "all results from FetchReviewRequests should have Role=reviewer")
	}
}

func TestMapCIStatus(t *testing.T) {
	tests := []struct {
		name     string
		state    githubv4.StatusState
		expected string
	}{
		{"success", githubv4.StatusStateSuccess, "passing"},
		{"failure", githubv4.StatusStateFailure, "failing"},
		{"error", githubv4.StatusStateError, "failing"},
		{"pending", githubv4.StatusStatePending, "pending"},
		{"expected", githubv4.StatusStateExpected, "unknown"},
		{"empty", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commits := []struct {
				Commit struct {
					StatusCheckRollup struct {
						State githubv4.StatusState
					}
				}
			}{
				{},
			}
			commits[0].Commit.StatusCheckRollup.State = tt.state
			assert.Equal(t, tt.expected, mapCIStatus(commits))
		})
	}
}

func TestMapCIStatus_EmptyCommits(t *testing.T) {
	var commits []struct {
		Commit struct {
			StatusCheckRollup struct {
				State githubv4.StatusState
			}
		}
	}
	assert.Equal(t, "unknown", mapCIStatus(commits))
}
