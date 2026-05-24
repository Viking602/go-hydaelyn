package memory_test

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/contract"
	"github.com/Viking602/go-hydaelyn/storage/memory"
)

// TestMemoryProvider_StoreProviderContract runs the framework's public
// contract test suite against the memory reference impl. The memory
// provider is the canonical correctness reference per ADR-012, so this
// suite MUST pass on every PR.
func TestMemoryProvider_StoreProviderContract(t *testing.T) {
	contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
		return memory.NewProvider(), func() {}
	})
}
