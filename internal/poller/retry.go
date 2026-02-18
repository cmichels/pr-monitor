package poller

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// retry calls fn up to maxAttempts times with exponential backoff (1s, 2s, 4s).
// It stops immediately on context cancellation, auth errors, or rate limit errors —
// only transient/network errors are retried.
func retry(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't retry auth or rate limit errors — they need special handling upstream.
		var authErr *AuthExpiredError
		var rlErr *RateLimitError
		if errors.As(lastErr, &authErr) || errors.As(lastErr, &rlErr) {
			return lastErr
		}

		if attempt == maxAttempts {
			break
		}

		slog.Warn("poll attempt failed, retrying",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"backoff", backoff,
			"error", lastErr,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
	}

	return lastErr
}
