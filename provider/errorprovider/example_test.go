package errorprovider_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/errorprovider"
)

// Example demonstrates injecting a deterministic provider failure — useful
// for unit tests or chaos experiments that need to exercise the runtime's
// error-handling paths without hitting a real upstream.
//
// The package supports three failure modes:
//   - [errorprovider.KindTimeout]       — block until ctx deadline / Delay
//   - [errorprovider.KindRateLimit]     — return [errorprovider.ErrRateLimit]
//   - [errorprovider.KindUpstreamError] — return [errorprovider.ErrUpstream]
func Example() {
	// Build a provider.Driver that always returns ErrRateLimit on Stream.
	driver := errorprovider.New(errorprovider.KindRateLimit)

	_, err := driver.Stream(context.Background(), provider.Request{})
	if errors.Is(err, errorprovider.ErrRateLimit) {
		fmt.Println("got ErrRateLimit")
	}
	// Output:
	// got ErrRateLimit
}
