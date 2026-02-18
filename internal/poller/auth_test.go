package poller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateScopes_HasReadOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "token test-token", r.Header.Get("Authorization"))
		w.Header().Set("X-OAuth-Scopes", "repo, read:org, user")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := validateScopesWithURL("test-token", srv.URL)
	assert.NoError(t, err)
}

func TestValidateScopes_MissingReadOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, user")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := validateScopesWithURL("test-token", srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read:org")
	assert.Contains(t, err.Error(), "team-based review requests")
}

func TestValidateScopes_OnlyReadOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "read:org")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := validateScopesWithURL("test-token", srv.URL)
	assert.NoError(t, err)
}

func TestValidateScopes_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := validateScopesWithURL("bad-token", srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid or expired")
}

func TestValidateScopes_EmptyScopes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No X-OAuth-Scopes header at all (e.g. GitHub App token)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := validateScopesWithURL("app-token", srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read:org")
}

func TestValidateScopes_NetworkError(t *testing.T) {
	// Use a URL that will refuse connection
	err := validateScopesWithURL("test-token", "http://127.0.0.1:1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reach GitHub API")
}
