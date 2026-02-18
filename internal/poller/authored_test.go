package poller

import (
	"context"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authoredPRNode builds a PR node with timeline items for authored PR test responses.
func authoredPRNode(id, repo string, number int, title, author, url string, files int, ciState string, timelineItems []map[string]interface{}) map[string]interface{} {
	pr := searchResultPR(id, repo, number, title, author, url, files, ciState)
	if timelineItems == nil {
		timelineItems = []map[string]interface{}{}
	}
	pr["timelineItems"] = map[string]interface{}{
		"nodes": timelineItems,
	}
	return pr
}

// reviewEvent builds a PullRequestReview timeline item.
func reviewEvent(login, state, createdAt string) map[string]interface{} {
	return map[string]interface{}{
		"__typename": "PullRequestReview",
		"author": map[string]interface{}{
			"login": login,
		},
		"state":     state,
		"createdAt": createdAt,
	}
}

// commentEvent builds an IssueComment timeline item.
func commentEvent(login, createdAt string) map[string]interface{} {
	return map[string]interface{}{
		"__typename": "IssueComment",
		"author": map[string]interface{}{
			"login": login,
		},
		"createdAt": createdAt,
	}
}

func TestFetchAuthoredPRs_WithApproval(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_1", "myorg/repo", 10, "My feature", "testuser",
					"https://github.com/myorg/repo/pull/10", 5, "SUCCESS",
					[]map[string]interface{}{
						reviewEvent("alice", "APPROVED", "2026-02-18T10:00:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "PR_1", results[0].PRID)
	assert.Equal(t, "author", results[0].Role)
	assert.Equal(t, "testuser", results[0].Author)
	require.NotNil(t, results[0].LastActivityAt)
	assert.Equal(t, "2026-02-18T10:00:00Z", results[0].LastActivityAt.Format("2006-01-02T15:04:05Z"))
	assert.Equal(t, "approved", *results[0].LastActivityType)
	assert.Equal(t, "alice", *results[0].LastActivityBy)
}

func TestFetchAuthoredPRs_WithComment(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_2", "myorg/repo", 20, "My bugfix", "testuser",
					"https://github.com/myorg/repo/pull/20", 2, "PENDING",
					[]map[string]interface{}{
						commentEvent("bob", "2026-02-17T15:30:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "author", results[0].Role)
	assert.Equal(t, "commented", *results[0].LastActivityType)
	assert.Equal(t, "bob", *results[0].LastActivityBy)
}

func TestFetchAuthoredPRs_WithChangesRequested(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_3", "myorg/repo", 30, "My refactor", "testuser",
					"https://github.com/myorg/repo/pull/30", 8, "FAILURE",
					[]map[string]interface{}{
						reviewEvent("carol", "CHANGES_REQUESTED", "2026-02-16T09:00:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "changes_requested", *results[0].LastActivityType)
	assert.Equal(t, "carol", *results[0].LastActivityBy)
}

func TestFetchAuthoredPRs_SelfActivityFiltered(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_4", "myorg/repo", 40, "WIP", "testuser",
					"https://github.com/myorg/repo/pull/40", 1, "SUCCESS",
					[]map[string]interface{}{
						// Self-review (should be ignored)
						reviewEvent("testuser", "COMMENTED", "2026-02-18T12:00:00Z"),
						// Self-comment (should be ignored)
						commentEvent("testuser", "2026-02-18T11:00:00Z"),
						// External review (should be picked up)
						reviewEvent("alice", "APPROVED", "2026-02-17T08:00:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	// Should pick alice's approval, NOT testuser's more recent activity
	assert.Equal(t, "approved", *results[0].LastActivityType)
	assert.Equal(t, "alice", *results[0].LastActivityBy)
	assert.Equal(t, "2026-02-17T08:00:00Z", results[0].LastActivityAt.Format("2006-01-02T15:04:05Z"))
}

func TestFetchAuthoredPRs_NoActivity(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_5", "myorg/repo", 50, "Fresh PR", "testuser",
					"https://github.com/myorg/repo/pull/50", 3, "PENDING",
					nil), // no timeline items
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "PR_5", results[0].PRID)
	assert.Equal(t, "author", results[0].Role)
	assert.Nil(t, results[0].LastActivityAt)
	assert.Nil(t, results[0].LastActivityType)
	assert.Nil(t, results[0].LastActivityBy)
}

func TestFetchAuthoredPRs_OnlySelfActivity(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_6", "myorg/repo", 60, "Solo work", "testuser",
					"https://github.com/myorg/repo/pull/60", 1, "SUCCESS",
					[]map[string]interface{}{
						reviewEvent("testuser", "COMMENTED", "2026-02-18T10:00:00Z"),
						commentEvent("testuser", "2026-02-18T09:00:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	// All activity was self — activity fields should be nil
	assert.Nil(t, results[0].LastActivityAt)
	assert.Nil(t, results[0].LastActivityType)
	assert.Nil(t, results[0].LastActivityBy)
}

func TestFetchAuthoredPRs_MostRecentWins(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_7", "myorg/repo", 70, "Active PR", "testuser",
					"https://github.com/myorg/repo/pull/70", 10, "SUCCESS",
					[]map[string]interface{}{
						// Older approval
						reviewEvent("alice", "APPROVED", "2026-02-15T10:00:00Z"),
						// Newer changes_requested (should win)
						reviewEvent("bob", "CHANGES_REQUESTED", "2026-02-17T14:00:00Z"),
						// Older comment
						commentEvent("carol", "2026-02-16T08:00:00Z"),
					}),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	// Bob's changes_requested is the most recent
	assert.Equal(t, "changes_requested", *results[0].LastActivityType)
	assert.Equal(t, "bob", *results[0].LastActivityBy)
	assert.Equal(t, "2026-02-17T14:00:00Z", results[0].LastActivityAt.Format("2006-01-02T15:04:05Z"))
}

func TestFetchAuthoredPRs_MultiplePRs(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_A", "myorg/repo", 1, "PR A", "testuser",
					"https://github.com/myorg/repo/pull/1", 2, "SUCCESS",
					[]map[string]interface{}{
						reviewEvent("alice", "APPROVED", "2026-02-18T10:00:00Z"),
					}),
				authoredPRNode("PR_B", "myorg/repo", 2, "PR B", "testuser",
					"https://github.com/myorg/repo/pull/2", 4, "PENDING",
					nil),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 2)
	// First PR has activity
	assert.Equal(t, "PR_A", results[0].PRID)
	assert.Equal(t, "author", results[0].Role)
	assert.Equal(t, "approved", *results[0].LastActivityType)
	// Second PR has no activity
	assert.Equal(t, "PR_B", results[1].PRID)
	assert.Equal(t, "author", results[1].Role)
	assert.Nil(t, results[1].LastActivityAt)
}

func TestFetchAuthoredPRs_AllRolesSetToAuthor(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse([]map[string]interface{}{
				authoredPRNode("PR_X", "myorg/repo", 1, "X", "testuser",
					"https://github.com/myorg/repo/pull/1", 1, "SUCCESS", nil),
				authoredPRNode("PR_Y", "myorg/repo", 2, "Y", "testuser",
					"https://github.com/myorg/repo/pull/2", 1, "SUCCESS", nil),
			}, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)

	for _, r := range results {
		assert.Equal(t, "author", r.Role, "all results from FetchAuthoredPRs should have Role=author")
	}
}

func TestFetchAuthoredPRs_EmptyResults(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		if strings.Contains(req.Query, "search") {
			return searchResponse(nil, false, "")
		}
		t.Fatalf("unexpected query: %s", req.Query)
		return nil
	})
	defer srv.Close()

	p := newTestPoller(t, srv.URL, "myorg", nil, "testuser")
	results, err := p.FetchAuthoredPRs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMapReviewState(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		expected string
	}{
		{"approved", "APPROVED", "approved"},
		{"changes_requested", "CHANGES_REQUESTED", "changes_requested"},
		{"commented", "COMMENTED", "commented"},
		{"dismissed", "DISMISSED", "commented"},
		{"pending", "PENDING", "commented"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapReviewState(githubv4.PullRequestReviewState(tt.state)))
		})
	}
}
