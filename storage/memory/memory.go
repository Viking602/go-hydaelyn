// Package memory provides the v0.8.0 reference implementation of
// api.StoreProvider that holds all state in process memory. It is the
// default provider used by hydaelyn.NewRuntime when no StoreProvider is
// configured, and is the canonical correctness reference for the contract
// test suite in contract/.
//
// The memory provider is intentionally simple: a single in-process
// transaction at a time (serialized via a gate), clone-on-Begin and
// replace-on-Commit. It self-declares the conservative capability profile
// — transactions yes, blackboard subscribe yes, list-pending yes,
// concurrent writers NO, dead-letter requeue NO. Production deployments
// should swap to one of the durable reference impls (storage/sqlite,
// storage/postgres, storage/mysql) or bring their own.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Reference
// implementations — memory".
package memory

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	internalmem "github.com/Viking602/go-hydaelyn/internal/memory"
)

// Provider is the public memory StoreProvider. It satisfies
// api.StoreProvider, api.BlackboardSubscriber, api.CapabilityReporter,
// and api.ProviderCloser.
type Provider struct {
	inner *internalmem.Provider
}

// NewProvider constructs a fresh in-process memory provider.
func NewProvider() *Provider {
	return &Provider{inner: internalmem.NewProvider()}
}

// Begin opens a new unit of work. Begin blocks until any in-flight
// transaction completes — the memory provider serializes writers.
func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return adapter.UnitOfWorkFromCore(uow), nil
}

// Subscribe returns a stream of BlackboardItems for the given run.
// Cancellation closes the channel and releases hub resources.
func (p *Provider) Subscribe(ctx context.Context, runID string, filter api.BlackboardSelector) (<-chan api.BlackboardItem, func() error, error) {
	items, cancel, err := p.inner.Subscribe(ctx, runID, adapter.BlackboardSelectorToModel(filter))
	if err != nil {
		return nil, nil, err
	}
	out := make(chan api.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- adapter.BlackboardItemFromModel(item)
		}
	}()
	return out, cancel, nil
}

// Capabilities self-declares the optional features this provider supports.
func (p *Provider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	caps, err := p.inner.Capabilities(ctx)
	if err != nil {
		return api.StoreCapabilities{}, err
	}
	return api.StoreCapabilities{
		SupportsTransactions:        caps.SupportsTransactions,
		SupportsBlackboardSubscribe: caps.SupportsBlackboardSubscribe,
		SupportsListPending:         caps.SupportsListPending,
		SupportsConcurrentWriters:   caps.SupportsConcurrentWriters,
		SupportsDeadLetterRequeue:   caps.SupportsDeadLetterRequeue,
	}, nil
}

// Close releases provider-scoped resources. For the memory provider this
// is a no-op — state is garbage-collected once the Provider goes out of
// scope.
func (p *Provider) Close(ctx context.Context) error {
	return p.inner.Close(ctx)
}
