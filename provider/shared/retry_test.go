package shared

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

// flakyTransport fails the first n round trips with a transport error,
// then delegates to the real transport.
type flakyTransport struct {
	failures int
	calls    atomic.Int32
	inner    http.RoundTripper
}

func (t *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if int(t.calls.Add(1)) <= t.failures {
		return nil, errors.New("transport: connection reset")
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
