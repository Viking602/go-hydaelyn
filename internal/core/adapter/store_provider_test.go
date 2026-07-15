package adapter

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/memory"
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
