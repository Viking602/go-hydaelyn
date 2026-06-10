package shared

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy bounds DoWithRetry. The zero value retries transient
// failures up to 3 total attempts with a 500ms base delay that doubles
// per retry, capped at 4s. MaxAttempts < 0 disables retrying entirely
// (a single attempt).
type RetryPolicy struct {
	// MaxAttempts is the total attempt count including the first call.
	// 0 means the default of 3; negative disables retrying.
	MaxAttempts int
	// BaseDelay is the wait before the first retry; it doubles on each
	// subsequent retry. 0 means 500ms.
	BaseDelay time.Duration
	// MaxDelay caps both the exponential backoff and any honored
	// Retry-After header. 0 means 4s.
	MaxDelay time.Duration
}

func (p RetryPolicy) attempts() int {
	switch {
	case p.MaxAttempts < 0:
		return 1
	case p.MaxAttempts == 0:
		return 3
	default:
		return p.MaxAttempts
	}
}

func (p RetryPolicy) baseDelay() time.Duration {
	if p.BaseDelay <= 0 {
		return 500 * time.Millisecond
	}
	return p.BaseDelay
}

func (p RetryPolicy) maxDelay() time.Duration {
	if p.MaxDelay <= 0 {
		return 4 * time.Second
	}
	return p.MaxDelay
}

// RetryableStatus reports whether an HTTP status is worth retrying:
// timeouts, rate limits, server errors, and provider overload (529).
func RetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	}
	return false
}

// DoWithRetry issues the request produced by build via client, retrying
// transport errors and RetryableStatus responses with exponential
// backoff (honoring a numeric Retry-After header up to the policy cap).
// build runs once per attempt so request bodies stay re-readable. The
// final attempt's response or error is returned unmodified, so callers
// keep their own status handling; failed retryable bodies are drained
// and closed before the next attempt.
func DoWithRetry(ctx context.Context, client *http.Client, policy RetryPolicy, build func() (*http.Request, error)) (*http.Response, error) {
	attempts := policy.attempts()
	delay := policy.baseDelay()
	for attempt := 1; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		retryable := err != nil || RetryableStatus(resp.StatusCode)
		if !retryable || attempt >= attempts {
			return resp, err
		}
		wait := delay
		if resp != nil {
			if after := retryAfter(resp); after > 0 {
				wait = after
			}
			// Drain a bounded prefix so the connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
			_ = resp.Body.Close()
		}
		if limit := policy.maxDelay(); wait > limit {
			wait = limit
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		// Clamp at the cap instead of doubling forever: unbounded doubling
		// overflows time.Duration after ~35 attempts, and a negative delay
		// would fire the timer immediately (a zero-interval retry storm).
		if delay < policy.maxDelay() {
			delay *= 2
		} else {
			delay = policy.maxDelay()
		}
	}
}

func retryAfter(resp *http.Response) time.Duration {
	seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
