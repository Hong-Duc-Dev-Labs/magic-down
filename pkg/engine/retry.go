package engine

import (
	"context"
	"net/http"
	"time"
)

const (
	retryBaseDelay = 500 * time.Millisecond
	retryMaxDelay  = 10 * time.Second
)

// retryDelay returns the exponential backoff for the given 1-based retry attempt.
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := retryBaseDelay << (attempt - 1)
	if d > retryMaxDelay || d <= 0 {
		d = retryMaxDelay
	}
	return d
}

// waitRetry sleeps for the backoff of attempt, returning early when ctx is done.
func waitRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(retryDelay(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryableStatus reports whether an HTTP status is worth retrying. Transient
// server failures and explicit throttling are retried; other 4xx are not.
func retryableStatus(code int) bool {
	return code >= 500 || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}
