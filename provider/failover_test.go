package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/errorprovider"
	"github.com/Viking602/go-hydaelyn/provider/scripted"
)

func TestFallback_UsesSecondaryWhenPrimaryFails(t *testing.T) {
	primary := errorprovider.New(errorprovider.KindUpstreamError)
	secondary := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "from secondary"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})

	driver := provider.Fallback(primary, secondary)
	stream, err := driver.Stream(context.Background(), provider.Request{
		Model:    "scripted",
		Messages: []message.Message{message.NewText(message.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v, want secondary to serve", err)
	}
	defer func() { _ = stream.Close() }()
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if event.Text != "from secondary" {
		t.Fatalf("event text = %q, want the secondary's output", event.Text)
	}
}

func TestFallback_AllFailingJoinsEveryCause(t *testing.T) {
	primary := errorprovider.New(errorprovider.KindUpstreamError)
	secondary := errorprovider.New(errorprovider.KindUpstreamError)

	driver := provider.Fallback(primary, secondary)
	_, err := driver.Stream(context.Background(), provider.Request{Model: "m"})
	if err == nil {
		t.Fatal("Stream() succeeded, want joined failure")
	}
	if !strings.Contains(err.Error(), "every fallback driver failed") {
		t.Fatalf("error %q does not mark fallback exhaustion", err)
	}
}

func TestFallback_MetadataReportsPrimary(t *testing.T) {
	primary := errorprovider.New(errorprovider.KindUpstreamError)
	secondary := scripted.New(nil)
	driver := provider.Fallback(primary, secondary)
	if got, want := driver.Metadata().Name, primary.Metadata().Name; got != want {
		t.Fatalf("Metadata().Name = %q, want primary's %q", got, want)
	}
}

func TestFallback_CancelledContextStopsChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	driver := provider.Fallback(errorprovider.New(errorprovider.KindUpstreamError), scripted.New(nil))
	_, err := driver.Stream(ctx, provider.Request{Model: "m"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stream() error = %v, want context.Canceled in the chain", err)
	}
}
