package poller

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClassifyError_Nil(t *testing.T) {
	assert.Nil(t, classifyError(nil))
}

func TestClassifyError_Unauthorized(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"contains 401", errors.New("non-200 OK status code: 401 Unauthorized body")},
		{"contains Unauthorized", errors.New("HTTP Unauthorized")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifyError(tt.err)
			var authErr *AuthExpiredError
			assert.True(t, errors.As(classified, &authErr), "should classify as AuthExpiredError")
		})
	}
}

func TestClassifyError_RateLimit(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"contains 403", errors.New("non-200 OK status code: 403 Forbidden body")},
		{"contains rate limit", errors.New("API rate limit exceeded")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifyError(tt.err)
			var rlErr *RateLimitError
			assert.True(t, errors.As(classified, &rlErr), "should classify as RateLimitError")
		})
	}
}

func TestClassifyError_TransientPassthrough(t *testing.T) {
	original := errors.New("connection refused")
	classified := classifyError(original)
	assert.Equal(t, original, classified, "should pass through unclassified errors")
}

func TestAuthExpiredError_Error(t *testing.T) {
	err := &AuthExpiredError{Msg: "test message"}
	assert.Equal(t, "test message", err.Error())
}

func TestRateLimitError_Error(t *testing.T) {
	resetAt := time.Date(2026, 2, 18, 15, 30, 0, 0, time.UTC)
	err := &RateLimitError{ResetAt: resetAt, Remaining: 5}
	msg := err.Error()
	assert.Contains(t, msg, "rate limited")
	assert.Contains(t, msg, "remaining: 5")
}

func TestCheckRateLimit_Normal(t *testing.T) {
	p := &Poller{}
	wait := p.checkRateLimit(100, time.Now().Add(time.Hour))
	assert.Equal(t, time.Duration(0), wait, "should not wait when remaining is healthy")
}

func TestCheckRateLimit_Low(t *testing.T) {
	p := &Poller{}
	resetAt := time.Now().Add(30 * time.Second)
	wait := p.checkRateLimit(5, resetAt)
	assert.Greater(t, wait, time.Duration(0), "should return positive wait when remaining < 10")
}

func TestCheckRateLimit_AlreadyReset(t *testing.T) {
	p := &Poller{}
	resetAt := time.Now().Add(-1 * time.Minute) // Already past.
	wait := p.checkRateLimit(5, resetAt)
	assert.Equal(t, time.Duration(0), wait, "should not wait if reset time already passed")
}
