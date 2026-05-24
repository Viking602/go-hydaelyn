package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/contract"
	"github.com/Viking602/go-hydaelyn/storage/postgres"
)

// Compile-time interface satisfaction.
var (
	_ api.StoreProvider      = (*postgres.Provider)(nil)
	_ api.CapabilityReporter = (*postgres.Provider)(nil)
	_ api.ProviderCloser     = (*postgres.Provider)(nil)
)

// TestPostgresProvider_StoreProviderContract runs the framework contract
// suite against a live Postgres instance. Skipped unless PG_TEST_DSN is
// set — CI should provision a per-job database and point this at it.
//
// Each subtest gets a fresh schema applied via AutoMigrate; the test
// teardown drops every table the schema created so reruns stay clean.
func TestPostgresProvider_StoreProviderContract(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping postgres contract suite")
	}

	contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
		p, err := postgres.NewProvider(postgres.Options{DSN: dsn, AutoMigrate: true})
		if err != nil {
			t.Fatalf("NewProvider: %v", err)
		}
		if err := truncateAll(context.Background(), p); err != nil {
			t.Fatalf("truncateAll: %v", err)
		}
		return p, func() {
			_ = truncateAll(context.Background(), p)
			_ = p.Close(context.Background())
		}
	})
}

// truncateAll wipes every contract-owned table so subtests start from a
// clean slate against a shared Postgres instance.
func truncateAll(ctx context.Context, p *postgres.Provider) error {
	tables := []string{
		"runs", "tasks", "events", "trace_spans", "blackboard_items",
		"user_messages", "envelopes", "leases", "approvals",
		"resume_tokens", "action_attempts", "agent_profiles",
		"capabilities", "usage_records", "dead_letters",
	}
	for _, tb := range tables {
		if _, err := p.DB().ExecContext(ctx, "TRUNCATE TABLE "+tb); err != nil {
			return err
		}
	}
	return nil
}
