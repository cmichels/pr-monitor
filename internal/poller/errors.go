package poller

import (
	"fmt"
	"time"
)

// AuthExpiredError is returned when GitHub responds with 401.
type AuthExpiredError struct {
	Msg string
}

func (e *AuthExpiredError) Error() string {
	return e.Msg
}

// RateLimitError is returned when we detect GitHub rate limiting.
type RateLimitError struct {
	ResetAt   time.Time
	Remaining int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub rate limited: resets at %s (remaining: %d)", e.ResetAt.Format(time.Kitchen), e.Remaining)
}
