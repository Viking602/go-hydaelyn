package errorprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
)

func TestErrorProviderRateLimit(t *testing.T) {
	driver := New(KindRateLimit)
	_, err := driver.Stream(context.Background(), provider.Request{})
	if !errors.Is(err, ErrRateLimit) {
		t.Fatalf("Stream() error = %v, want ErrRateLimit", err)
	}
}

func TestErrorProviderUpstream(t *testing.T) {
	driver := New(KindUpstreamError)
	_, err := driver.Stream(context.Background(), provider.Request{})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Stream() error = %v, want ErrUpstream", err)
	}
}

func TestErrorProviderTimeout(t *testing.T) {
	driver := &ErrorProvider{Failure: KindTimeout, Delay: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := driver.Stream(ctx, provider.Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stream() error = %v, want context.DeadlineExceeded", err)
	}
}
