package sqlite_test

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/contract"
	"github.com/Viking602/go-hydaelyn/storage/sqlite"
)

// Compile-time interface satisfaction.
var (
	_ api.StoreProvider      = (*sqlite.Provider)(nil)
	_ api.CapabilityReporter = (*sqlite.Provider)(nil)
	_ api.ProviderCloser     = (*sqlite.Provider)(nil)
)

// TestSQLiteProvider_StoreProviderContract runs the full framework
// contract suite against the SQLite reference impl. Per the v0.8.0 spec
// every reference implementation must pass this suite on each PR.
func TestSQLiteProvider_StoreProviderContract(t *testing.T) {
	contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
		p, err := sqlite.NewProvider(sqlite.Options{DSN: ":memory:", AutoMigrate: true})
		if err != nil {
			t.Fatalf("NewProvider: %v", err)
		}
		return p, func() { _ = p.Close(context.Background()) }
	})
}
