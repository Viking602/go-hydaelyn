package shared

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	rand "math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/Viking602/venat/provider"
)

const maxRetryAttempts = 10

// RetryPolicy bounds DoWithRetry. The zero value retries transient
// failures up to 3 total attempts with equal jitter over an exponential 500ms
// base delay, capped at 4s. MaxAttempts < 0 disables retrying entirely (a
// single attempt); values above 10 are clamped to prevent retry storms.
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
	// Jitter transforms each exponential delay before a server Retry-After
	// minimum is applied. Nil applies equal jitter in [delay/2, delay].
	// Use an identity function for deterministic delays.
	Jitter func(time.Duration) time.Duration
}

func (p RetryPolicy) attempts() int {
	switch {
	case p.MaxAttempts < 0:
		return 1
	case p.MaxAttempts == 0:
		return 3
	default:
		return min(p.MaxAttempts, maxRetryAttempts)
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

// RetryableStatus reports whether an HTTP status is a rate limit or server
// failure. Client errors other than 429 are deterministic request failures and
// must not be replayed.
func RetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
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
		if attempt == 1 && !retrySafeRequest(req) {
			attempts = 1
		}
		resp, err := client.Do(req)
		retryable := err != nil && (provider.IsRetryableError(err) || ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded)) ||
			err == nil && RetryableStatus(resp.StatusCode)
		if !retryable || attempt >= attempts {
			return resp, err
		}
		wait := equalJitter(delay)
		if policy.Jitter != nil {
			wait = max(time.Duration(0), policy.Jitter(delay))
		}
		if resp != nil {
			if after := retryAfter(resp); after > wait {
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
		// Saturate before doubling. Comparing to half the cap avoids
		// time.Duration overflow even when callers use a near-MaxDuration cap.
		limit := policy.maxDelay()
		if delay >= limit/2 {
			delay = limit
		} else {
			delay *= 2
		}
	}
}

func equalJitter(delay time.Duration) time.Duration {
	if delay <= 1 {
		return max(time.Duration(0), delay)
	}
	lower := delay / 2
	return lower + time.Duration(rand.Int64N(int64(delay-lower)+1))
}

func retryAfter(resp *http.Response) time.Duration {
	value := resp.Header.Get("Retry-After")
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(at)
	if delay <= 0 {
		return 0
	}
	return delay
}

func retrySafeRequest(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return req.Header.Get("Idempotency-Key") != ""
	}
}

// NewIdempotencyKey returns an opaque key callers can retain across transport
// retries of one logical request.
func NewIdempotencyKey() (string, error) {
	var random [16]byte
	if _, err := cryptorand.Read(random[:]); err != nil {
		return "", err
	}
	return "venat-" + hex.EncodeToString(random[:]), nil
}
