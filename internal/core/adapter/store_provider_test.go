package adapter

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/memory"
)

func TestStoreProviderAdaptersPreserveCapabilitiesAndClose(t *testing.T) {
	ctx := context.Background()
	public := StoreProviderFromCore(memory.NewProvider())
	reporter, ok := public.(api.CapabilityReporter)
	if !ok {
		t.Fatal("StoreProviderFromCore dropped CapabilityReporter")
	}
	capabilities, err := reporter.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !capabilities.SupportsTransactions || !capabilities.SupportsBlackboardSubscribe {
		t.Fatalf("Capabilities() = %#v, want memory provider capabilities", capabilities)
	}
	closer, ok := public.(api.ProviderCloser)
	if !ok {
		t.Fatal("StoreProviderFromCore dropped ProviderCloser")
	}
	if err := closer.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	internal := StoreProviderToCore(public)
	internalReporter, ok := internal.(core.CapabilityReporter)
	if !ok {
		t.Fatal("StoreProviderToCore dropped CapabilityReporter")
	}
	internalCapabilities, err := internalReporter.Capabilities(ctx)
	if err != nil {
		t.Fatalf("internal Capabilities() error = %v", err)
	}
	if !internalCapabilities.SupportsTransactions || !internalCapabilities.SupportsBlackboardSubscribe {
		t.Fatalf("internal Capabilities() = %#v, want memory provider capabilities", internalCapabilities)
	}
	internalCloser, ok := internal.(core.ProviderCloser)
	if !ok {
		t.Fatal("StoreProviderToCore dropped ProviderCloser")
	}
	if err := internalCloser.Close(ctx); err != nil {
		t.Fatalf("internal Close() error = %v", err)
	}
}

func TestSubscribeAdapterUnblocksWhenConsumerStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := make(chan api.BlackboardItem)
	provider := subscribeStub{ch: upstream}
	adapted, ok := StoreProviderToCore(provider).(core.BlackboardSubscriber)
	if !ok {
		t.Fatal("StoreProviderToCore dropped BlackboardSubscriber")
	}
	out, stop, err := adapted.Subscribe(ctx, "run-1", model.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	upstream <- api.BlackboardItem{ID: "item-1", RunID: "run-1"}
	if got := <-out; got.ID != "item-1" {
		t.Fatalf("forwarded item = %#v", got)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop() error = %v", err)
	}
	// A send that would otherwise block the unbuffered adapter must not hang
	// after the consumer cancelled.
	sent := make(chan struct{})
	go func() {
		upstream <- api.BlackboardItem{ID: "item-2", RunID: "run-1"}
		close(sent)
	}()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("adapter forwarder stayed blocked after cancel")
	}
}

type subscribeStub struct {
	ch chan api.BlackboardItem
}

func (subscribeStub) Begin(context.Context) (api.UnitOfWork, error) {
	return nil, nil
}

func (s subscribeStub) Subscribe(context.Context, string, api.BlackboardSelector) (<-chan api.BlackboardItem, func() error, error) {
	return s.ch, func() error { return nil }, nil
}
