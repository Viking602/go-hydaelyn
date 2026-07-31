package contract_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/contract"
	"github.com/Viking602/venat/contract/internal/inmemfake"
)

// TestContractSuite_SelfCheck runs the public contract suite against the
// framework's non-exported in-memory adapter. This proves the suite
// compiles, runs end-to-end, and stays internally consistent on every PR.
// The adapter is unreachable from user code (Go internal/ rule) — it is
// NOT a reference implementation.
//
// Per ADR-012 (revised, Position D) the framework ships no public
// StoreProvider implementation. This self-test guards the contract
// suite itself; it does not endorse the adapter as a backend.
func TestContractSuite_SelfCheck(t *testing.T) {
	contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
		return inmemfake.NewProvider(), func() {}
	})
	contract.RunRecoveryContractTests(t, func(t *testing.T) (func() api.StoreProvider, func()) {
		shared := inmemfake.NewProvider()
		return func() api.StoreProvider {
				return shared.Reopen()
			}, func() {
				if err := shared.Close(context.Background()); err != nil {
					t.Errorf("recovery provider cleanup error = %v", err)
				}
			}
	})
}
