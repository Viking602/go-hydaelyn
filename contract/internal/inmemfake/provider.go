// Package inmemfake provides a non-exported in-memory api.StoreProvider
// used solely by the framework's own contract-suite self-test in
// contract/contract_test.go. It lives under contract/internal/ so it is
// structurally unreachable from any package outside contract/ — Go's
// internal/ rule enforces this at compile time.
//
// It is NOT a storage backend, NOT a starting point for forking, and NOT
// listed in any user-facing recommendation. It exists for one reason: to
// let framework CI exercise contract.RunStoreProviderContractTests on
// every PR so the suite cannot rot silently.
//
// Per ADR-012 (revised, Position D) the framework ships no public
// api.StoreProvider implementation. Applications implement the contract
// against their own data stack (ent / gorm / sqlc / DBA-controlled DDL).
// See docs/product-spec/v0.8.0/12-migration-guide.md for the recommended
// template.
package inmemfake

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	internalmem "github.com/Viking602/go-hydaelyn/internal/memory"
)

// Provider is the in-memory api.StoreProvider used by the framework's
// contract self-test. It satisfies api.StoreProvider,
// api.BlackboardSubscriber, api.CapabilityReporter, and api.ProviderCloser.
type Provider struct {
	inner *internalmem.Provider
}

// NewProvider constructs a fresh provider with empty state.
func NewProvider() *Provider {
	return &Provider{inner: internalmem.NewProvider()}
}

// Reopen returns a distinct provider wrapper sharing this provider's committed
// in-memory state. It is used only to simulate a new runtime over one store.
func (p *Provider) Reopen() *Provider {
	return &Provider{inner: p.inner}
}

// Begin opens a new unit of work. Writes are serialized — concurrent
// Begin calls block until the previous UoW closes.
func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return adapter.UnitOfWorkFromCore(uow), nil
}

// Subscribe streams blackboard items for the given run. The returned
// channel closes when the cancel func is called or ctx is cancelled.
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
// Same profile as the prior storage/memory wrapper: transactions, list-pending,
// and blackboard subscribe are supported; concurrent writers and dead-letter
// requeue are not.
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

// Close releases provider-scoped resources. State is GC'd once Provider
// goes out of scope, so this is effectively a no-op.
func (p *Provider) Close(ctx context.Context) error {
	return p.inner.Close(ctx)
}
