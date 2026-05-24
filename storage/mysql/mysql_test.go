package mysql_test

import (
	"context"
	"os"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/contract"
	"github.com/Viking602/go-hydaelyn/storage/mysql"
)

// Compile-time interface satisfaction.
var (
	_ api.StoreProvider      = (*mysql.Provider)(nil)
	_ api.CapabilityReporter = (*mysql.Provider)(nil)
	_ api.ProviderCloser     = (*mysql.Provider)(nil)
)

// TestMySQLProvider_StoreProviderContract runs the framework contract
// suite against a live MySQL-family instance. Skipped unless
// MYSQL_TEST_DSN is set — CI should provision a per-job database and
// point this at it. The same DSN works against MariaDB, TiDB, and
// OceanBase (MySQL mode); the suite is the cross-engine smoke test.
func TestMySQLProvider_StoreProviderContract(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set; skipping mysql contract suite")
	}

	contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
		p, err := mysql.NewProvider(mysql.Options{DSN: dsn, AutoMigrate: true})
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

func truncateAll(ctx context.Context, p *mysql.Provider) error {
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
