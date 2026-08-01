package shared

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func buildGet(t *testing.T, url string) func() (*http.Request, error) {
	t.Helper()
	return func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	}
}

func TestDoWithRetry_RetriesTransientStatusThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	policy := RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	resp, err := DoWithRetry(context.Background(), server.Client(), policy, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after retries", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("server saw %d calls, want 3 (two 429 retries)", got)
	}
}

func TestDoWithRetry_RequiresIdempotentRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	build := func(withKey bool) func() (*http.Request, error) {
		return func() (*http.Request, error) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
			if withKey {
				req.Header.Set("Idempotency-Key", "logical-request-1")
			}
			return req, err
		}
	}
	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: time.Nanosecond, MaxDelay: time.Nanosecond}
	resp, err := DoWithRetry(context.Background(), server.Client(), policy, build(false))
	if err != nil {
		t.Fatalf("non-idempotent DoWithRetry error = %v", err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 1 {
		t.Fatalf("non-idempotent POST calls = %d, want one", got)
	}

	calls.Store(0)
	resp, err = DoWithRetry(context.Background(), server.Client(), policy, build(true))
	if err != nil {
		t.Fatalf("idempotent DoWithRetry error = %v", err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 3 {
		t.Fatalf("idempotent POST calls = %d, want three", got)
	}
}

func TestRetryableStatus_OnlyRateLimitAndServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusNotImplemented, 529} {
		if !RetryableStatus(status) {
			t.Fatalf("RetryableStatus(%d) = false", status)
		}
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusConflict, http.StatusUnprocessableEntity} {
		if RetryableStatus(status) {
			t.Fatalf("RetryableStatus(%d) = true", status)
		}
	}
}

func TestRetryAfter_AcceptsHTTPDate(t *testing.T) {
	future := time.Now().Add(2 * time.Second).UTC().Truncate(time.Second)
	resp := &http.Response{Header: http.Header{"Retry-After": []string{future.Format(http.TimeFormat)}}}
	delay := retryAfter(resp)
	if delay <= 0 || delay > 2*time.Second {
		t.Fatalf("retryAfter(HTTP date) = %v, want positive delay up to two seconds", delay)
	}
}

func TestDoWithRetry_NonRetryableStatusPassesThroughImmediately(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	resp, err := DoWithRetry(context.Background(), server.Client(), RetryPolicy{BaseDelay: time.Millisecond}, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want the 400 passed through", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d calls, want 1 (400 is not retryable)", got)
	}
}

func TestDoWithRetry_ExhaustionReturnsLastResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	policy := RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond}
	resp, err := DoWithRetry(context.Background(), server.Client(), policy, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want the final 503 returned unmodified", resp.StatusCode)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server saw %d calls, want MaxAttempts=2", got)
	}
}

func TestDoWithRetry_DisabledRetriesMakesOneAttempt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	resp, err := DoWithRetry(context.Background(), server.Client(), RetryPolicy{MaxAttempts: -1}, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d calls, want 1 with retries disabled", got)
	}
}

func TestDoWithRetry_AppliesConfiguredJitter(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var jitterInput time.Duration
	policy := RetryPolicy{
		BaseDelay: time.Hour,
		MaxDelay:  time.Hour,
		Jitter: func(delay time.Duration) time.Duration {
			jitterInput = delay
			return 0
		},
	}
	resp, err := DoWithRetry(context.Background(), server.Client(), policy, buildGet(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if jitterInput != time.Hour || calls.Load() != 2 {
		t.Fatalf("jitter input=%s calls=%d", jitterInput, calls.Load())
	}
}

func TestDoWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	policy := RetryPolicy{BaseDelay: time.Hour, MaxDelay: time.Hour}
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := DoWithRetry(ctx, server.Client(), policy, buildGet(t, server.URL))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DoWithRetry error = %v, want context.Canceled during backoff", err)
	}
}

func TestDoWithRetry_ClampsExcessiveAttemptCount(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	resp, err := DoWithRetry(context.Background(), server.Client(), RetryPolicy{
		MaxAttempts: 1000,
		BaseDelay:   time.Nanosecond,
		MaxDelay:    time.Nanosecond,
	}, buildGet(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if calls.Load() != maxRetryAttempts {
		t.Fatalf("retry attempts = %d, want capped %d", calls.Load(), maxRetryAttempts)
	}
}

func TestDoWithRetry_SaturatesBackoffWithoutDurationOverflow(t *testing.T) {
	var calls atomic.Int32
	var jitterInputs []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	limit := time.Duration(1<<63 - 1)
	resp, err := DoWithRetry(context.Background(), server.Client(), RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   limit/2 + 1,
		MaxDelay:    limit,
		Jitter: func(delay time.Duration) time.Duration {
			jitterInputs = append(jitterInputs, delay)
			return 0
		},
	}, buildGet(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if calls.Load() != 3 || len(jitterInputs) != 2 ||
		jitterInputs[0] != limit/2+1 || jitterInputs[1] != limit {
		t.Fatalf("calls=%d jitter inputs=%v, want positive saturated backoff", calls.Load(), jitterInputs)
	}
}

// flakyTransport fails the first n round trips with a transport error,
// then delegates to the real transport.
type flakyTransport struct {
	failures int
	calls    atomic.Int32
	inner    http.RoundTripper
	failure  error
}

func (t *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if int(t.calls.Add(1)) <= t.failures {
		failure := t.failure
		if failure == nil {
			failure = syscall.ECONNRESET
		}
		return nil, fmt.Errorf("transport: %w", failure)
	}
	return t.inner.RoundTrip(req)
}

func TestDoWithRetry_RetriesTransportErrorThenSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &flakyTransport{failures: 2, inner: server.Client().Transport}
	client := &http.Client{Transport: transport}
	policy := RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	resp, err := DoWithRetry(context.Background(), client, policy, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v, want success after two transport-error retries", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := transport.calls.Load(); got != 3 {
		t.Fatalf("transport saw %d calls, want 3", got)
	}
}

func TestDoWithRetry_RetriesClientTimeoutWhileParentContextLives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &flakyTransport{
		failures: 1,
		failure:  context.DeadlineExceeded,
		inner:    server.Client().Transport,
	}
	client := &http.Client{Transport: transport}
	policy := RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	resp, err := DoWithRetry(context.Background(), client, policy, buildGet(t, server.URL))
	if err != nil {
		t.Fatalf("DoWithRetry error = %v, want recovery from client timeout", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if transport.calls.Load() != 2 {
		t.Fatalf("transport calls = %d, want 2", transport.calls.Load())
	}
}
