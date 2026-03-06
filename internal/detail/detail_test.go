package detail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrismichels/pr-monitor/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

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

func newErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{
				{"message": "Something went wrong"},
			},
		})
	}))
}

type nodeResponseOpts struct {
	body           string
	files          []map[string]interface{}
	checks         []map[string]interface{}
	comments       []map[string]interface{}
	reviews        []map[string]interface{}
	reviewRequests []map[string]interface{}
}

func nodeResponse(body string, files []map[string]interface{}, checks []map[string]interface{}) map[string]interface{} {
	return nodeResponseFull(nodeResponseOpts{body: body, files: files, checks: checks})
}

func nodeResponseFull(opts nodeResponseOpts) map[string]interface{} {
	contexts := opts.checks
	if contexts == nil {
		contexts = []map[string]interface{}{}
	}

	fileNodes := opts.files
	if fileNodes == nil {
		fileNodes = []map[string]interface{}{}
	}

	commentNodes := opts.comments
	if commentNodes == nil {
		commentNodes = []map[string]interface{}{}
	}

	reviewNodes := opts.reviews
	if reviewNodes == nil {
		reviewNodes = []map[string]interface{}{}
	}

	reviewRequestNodes := opts.reviewRequests
	if reviewRequestNodes == nil {
		reviewRequestNodes = []map[string]interface{}{}
	}

	return map[string]interface{}{
		"node": map[string]interface{}{
			"body": opts.body,
			"files": map[string]interface{}{
				"nodes": fileNodes,
			},
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": contexts,
								},
							},
						},
					},
				},
			},
			"comments": map[string]interface{}{
				"nodes": commentNodes,
			},
			"reviews": map[string]interface{}{
				"nodes": reviewNodes,
			},
			"reviewRequests": map[string]interface{}{
				"nodes": reviewRequestNodes,
			},
		},
	}
}

func TestFetchDetail_Success(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponse(
			"## Description\nFixes a bug",
			[]map[string]interface{}{
				{"path": "main.go", "additions": 10, "deletions": 3},
				{"path": "main_test.go", "additions": 20, "deletions": 0},
			},
			[]map[string]interface{}{
				{
					"__typename": "CheckRun",
					"name":       "CI / build",
					"status":     "COMPLETED",
					"conclusion": "SUCCESS",
				},
			},
		)
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_123")
	require.NoError(t, err)

	assert.Equal(t, "## Description\nFixes a bug", detail.Body)
	require.Len(t, detail.Files, 2)
	assert.Equal(t, "main.go", detail.Files[0].Path)
	assert.Equal(t, 10, detail.Files[0].Additions)
	assert.Equal(t, 3, detail.Files[0].Deletions)
	assert.Equal(t, "main_test.go", detail.Files[1].Path)

	require.Len(t, detail.Checks, 1)
	assert.Equal(t, "CI / build", detail.Checks[0].Name)
	assert.Equal(t, "COMPLETED", detail.Checks[0].Status)
	assert.Equal(t, "SUCCESS", detail.Checks[0].Conclusion)
}

func TestFetchDetail_NoFiles(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponse("Empty PR", nil, nil)
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_EMPTY")
	require.NoError(t, err)

	assert.Equal(t, "Empty PR", detail.Body)
	assert.Empty(t, detail.Files)
	assert.Empty(t, detail.Checks)
}

func TestFetchDetail_NoChecks(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponse(
			"PR with files but no checks",
			[]map[string]interface{}{
				{"path": "README.md", "additions": 1, "deletions": 1},
			},
			nil,
		)
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_NOCHECK")
	require.NoError(t, err)

	require.Len(t, detail.Files, 1)
	assert.Empty(t, detail.Checks)
}

func TestFetchDetail_MixedCheckTypes(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponse(
			"Mixed checks",
			nil,
			[]map[string]interface{}{
				{
					"__typename": "CheckRun",
					"name":       "build",
					"status":     "COMPLETED",
					"conclusion": "SUCCESS",
				},
				{
					"__typename": "StatusContext",
					"context":    "ci/external",
					"state":      "SUCCESS",
				},
				{
					"__typename": "CheckRun",
					"name":       "lint",
					"status":     "IN_PROGRESS",
					"conclusion": "",
				},
			},
		)
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_MIXED")
	require.NoError(t, err)

	require.Len(t, detail.Checks, 3)

	assert.Equal(t, "build", detail.Checks[0].Name)
	assert.Equal(t, "SUCCESS", detail.Checks[0].Conclusion)

	assert.Equal(t, "ci/external", detail.Checks[1].Name)
	assert.Equal(t, "COMPLETED", detail.Checks[1].Status)
	assert.Equal(t, "SUCCESS", detail.Checks[1].Conclusion)

	assert.Equal(t, "lint", detail.Checks[2].Name)
	assert.Equal(t, "IN_PROGRESS", detail.Checks[2].Status)
}

func TestFetchDetail_GraphQLError(t *testing.T) {
	srv := newErrorServer(t)
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	_, err := f.FetchDetail(context.Background(), "PR_BAD")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetch PR detail")
}

func TestFetchDetail_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": nodeResponse("slow", nil, nil),
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	_, err := f.FetchDetail(ctx, "PR_SLOW")
	assert.Error(t, err)
}

func TestFetchDetail_CommentsAndReviews(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponseFull(nodeResponseOpts{
			body: "PR with discussion",
			comments: []map[string]interface{}{
				{
					"author":    map[string]interface{}{"login": "alice"},
					"body":      "Looks good overall",
					"createdAt": "2025-01-15T10:00:00Z",
				},
				{
					"author":    map[string]interface{}{"login": "bob"},
					"body":      "Can you add tests?",
					"createdAt": "2025-01-15T11:00:00Z",
				},
			},
			reviews: []map[string]interface{}{
				{
					"author":    map[string]interface{}{"login": "alice"},
					"body":      "LGTM",
					"state":     "APPROVED",
					"createdAt": "2025-01-15T12:00:00Z",
				},
				{
					"author":    map[string]interface{}{"login": "charlie"},
					"body":      "",
					"state":     "CHANGES_REQUESTED",
					"createdAt": "2025-01-15T09:00:00Z",
				},
			},
		})
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_COMMENTS")
	require.NoError(t, err)

	// 2 comments + 2 reviews = 4 entries, sorted chronologically.
	require.Len(t, detail.Comments, 4)

	// Oldest first: charlie's review at 09:00.
	assert.Equal(t, "charlie", detail.Comments[0].Author)
	assert.Equal(t, "CHANGES_REQUESTED", detail.Comments[0].ReviewState)
	assert.Equal(t, "Changes requested", detail.Comments[0].Body) // stateLabel fallback

	// alice's comment at 10:00.
	assert.Equal(t, "alice", detail.Comments[1].Author)
	assert.Equal(t, "Looks good overall", detail.Comments[1].Body)
	assert.Empty(t, detail.Comments[1].ReviewState)

	// bob's comment at 11:00.
	assert.Equal(t, "bob", detail.Comments[2].Author)
	assert.Equal(t, "Can you add tests?", detail.Comments[2].Body)

	// alice's approval at 12:00.
	assert.Equal(t, "alice", detail.Comments[3].Author)
	assert.Equal(t, "LGTM", detail.Comments[3].Body)
	assert.Equal(t, "APPROVED", detail.Comments[3].ReviewState)
}

func TestFetchDetail_NoComments(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponseFull(nodeResponseOpts{body: "No discussion"})
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_QUIET")
	require.NoError(t, err)

	assert.Empty(t, detail.Comments)
}

func TestFetchDetail_ExcludesSwarmiaBot(t *testing.T) {
	srv := newTestServer(t, func(req graphQLRequest) interface{} {
		return nodeResponseFull(nodeResponseOpts{
			body: "Bot filtered",
			comments: []map[string]interface{}{
				{
					"author":    map[string]interface{}{"login": "swarmia"},
					"body":      "Bot summary",
					"createdAt": "2025-01-15T09:00:00Z",
				},
				{
					"author":    map[string]interface{}{"login": "alice"},
					"body":      "Real comment",
					"createdAt": "2025-01-15T10:00:00Z",
				},
				{
					"author":    map[string]interface{}{"login": "swarmia-app[bot]"},
					"body":      "Another bot comment",
					"createdAt": "2025-01-15T11:00:00Z",
				},
			},
			reviews: []map[string]interface{}{
				{
					"author":    map[string]interface{}{"login": "Swarmia"},
					"body":      "Bot review",
					"state":     "COMMENTED",
					"createdAt": "2025-01-15T08:00:00Z",
				},
				{
					"author":    map[string]interface{}{"login": "bob"},
					"body":      "LGTM",
					"state":     "APPROVED",
					"createdAt": "2025-01-15T12:00:00Z",
				},
			},
		})
	})
	defer srv.Close()

	f := newFetcherWithHTTPClient(srv.URL, http.DefaultClient)
	detail, err := f.FetchDetail(context.Background(), "PR_BOT")
	require.NoError(t, err)

	// Only alice and bob should remain.
	require.Len(t, detail.Comments, 2)
	assert.Equal(t, "alice", detail.Comments[0].Author)
	assert.Equal(t, "bob", detail.Comments[1].Author)
}

// --- computeReviewStatuses unit tests ---

func TestComputeReviewStatuses_LatestReviewWins(t *testing.T) {
	// alice first requests changes, then approves => latest state is APPROVED
	reviews := []reviewInput{
		{Author: "alice", State: "CHANGES_REQUESTED"},
		{Author: "alice", State: "APPROVED"},
	}

	result := computeReviewStatuses(reviews, nil)

	require.Len(t, result, 1)
	assert.Equal(t, "alice", result[0].Author)
	assert.Equal(t, "APPROVED", result[0].State)
}

func TestComputeReviewStatuses_PendingFromRequests(t *testing.T) {
	// carol is in reviewRequests but has no reviews => PENDING
	requests := []requestInput{
		{Name: "carol"},
	}

	result := computeReviewStatuses(nil, requests)

	require.Len(t, result, 1)
	assert.Equal(t, "carol", result[0].Author)
	assert.Equal(t, "PENDING", result[0].State)
}

func TestComputeReviewStatuses_MixedReviewsAndRequests(t *testing.T) {
	// alice approved, bob requested changes, carol still pending
	reviews := []reviewInput{
		{Author: "alice", State: "APPROVED"},
		{Author: "bob", State: "CHANGES_REQUESTED"},
	}
	requests := []requestInput{
		{Name: "carol"},
	}

	result := computeReviewStatuses(reviews, requests)

	require.Len(t, result, 3)
	// Sort order: changes_requested(0) > pending(1) > approved(3)
	assert.Equal(t, tui.ReviewStatus{Author: "bob", State: "CHANGES_REQUESTED"}, result[0])
	assert.Equal(t, tui.ReviewStatus{Author: "carol", State: "PENDING"}, result[1])
	assert.Equal(t, tui.ReviewStatus{Author: "alice", State: "APPROVED"}, result[2])
}

func TestComputeReviewStatuses_DismissedIgnored(t *testing.T) {
	// alice's review was dismissed => she's excluded (no active review state)
	reviews := []reviewInput{
		{Author: "alice", State: "DISMISSED"},
	}

	result := computeReviewStatuses(reviews, nil)

	assert.Empty(t, result)
}

func TestComputeReviewStatuses_DismissedThenApproved(t *testing.T) {
	// alice was dismissed then approved => APPROVED (dismissed is skipped, approved overwrites)
	reviews := []reviewInput{
		{Author: "alice", State: "APPROVED"},
		{Author: "alice", State: "DISMISSED"},
		{Author: "alice", State: "APPROVED"},
	}

	result := computeReviewStatuses(reviews, nil)

	require.Len(t, result, 1)
	assert.Equal(t, "APPROVED", result[0].State)
}

func TestComputeReviewStatuses_ReviewOverridesPending(t *testing.T) {
	// alice is in reviewRequests AND has a review => review state wins
	reviews := []reviewInput{
		{Author: "alice", State: "COMMENTED"},
	}
	requests := []requestInput{
		{Name: "alice"},
	}

	result := computeReviewStatuses(reviews, requests)

	require.Len(t, result, 1)
	assert.Equal(t, "COMMENTED", result[0].State)
}

func TestComputeReviewStatuses_TeamReviewRequest(t *testing.T) {
	// Team review requests use the team name instead of login
	requests := []requestInput{
		{Name: "frontend-team"},
	}

	result := computeReviewStatuses(nil, requests)

	require.Len(t, result, 1)
	assert.Equal(t, "frontend-team", result[0].Author)
	assert.Equal(t, "PENDING", result[0].State)
}

func TestComputeReviewStatuses_BotsExcluded(t *testing.T) {
	reviews := []reviewInput{
		{Author: "swarmia", State: "COMMENTED"},
		{Author: "real-dev", State: "APPROVED"},
	}

	result := computeReviewStatuses(reviews, nil)

	require.Len(t, result, 1)
	assert.Equal(t, "real-dev", result[0].Author)
}

func TestComputeReviewStatuses_EmptyInputs(t *testing.T) {
	result := computeReviewStatuses(nil, nil)
	assert.Empty(t, result)
}

func TestComputeReviewStatuses_SortOrder(t *testing.T) {
	// Verify full sort order: changes_requested, pending, commented, approved
	// When states are the same, sort alphabetically by author
	reviews := []reviewInput{
		{Author: "dave", State: "APPROVED"},
		{Author: "alice", State: "COMMENTED"},
		{Author: "bob", State: "CHANGES_REQUESTED"},
		{Author: "eve", State: "APPROVED"},
	}
	requests := []requestInput{
		{Name: "carol"},
	}

	result := computeReviewStatuses(reviews, requests)

	require.Len(t, result, 5)
	assert.Equal(t, "bob", result[0].Author)       // CHANGES_REQUESTED
	assert.Equal(t, "carol", result[1].Author)      // PENDING
	assert.Equal(t, "alice", result[2].Author)       // COMMENTED
	assert.Equal(t, "dave", result[3].Author)        // APPROVED
	assert.Equal(t, "eve", result[4].Author)         // APPROVED
}
