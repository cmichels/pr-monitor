package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, func() error {
		calls++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetry_SucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, func() error {
		calls++
		if calls < 3 {
			return errors.New("network timeout")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetry_ExhaustsAttempts(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, func() error {
		calls++
		return errors.New("persistent failure")
	})

	assert.Error(t, err)
	assert.Equal(t, 3, calls)
	assert.Contains(t, err.Error(), "persistent failure")
}

func TestRetry_StopsImmediatelyOnAuthError(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, func() error {
		calls++
		return &AuthExpiredError{Msg: "token expired"}
	})

	assert.Error(t, err)
	assert.Equal(t, 1, calls, "should not retry auth errors")
	var authErr *AuthExpiredError
	assert.True(t, errors.As(err, &authErr))
}

func TestRetry_StopsImmediatelyOnRateLimitError(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, func() error {
		calls++
		return &RateLimitError{ResetAt: time.Now().Add(5 * time.Minute), Remaining: 0}
	})

	assert.Error(t, err)
	assert.Equal(t, 1, calls, "should not retry rate limit errors")
	var rlErr *RateLimitError
	assert.True(t, errors.As(err, &rlErr))
}

func TestRetry_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	calls := 0
	err := retry(ctx, 3, func() error {
		calls++
		return errors.New("should not retry")
	})

	assert.Error(t, err)
	// Should either get the original error (first attempt) or context.Canceled
	assert.LessOrEqual(t, calls, 1)
}
