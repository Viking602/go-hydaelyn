// Package postgres is the v0.8.0 PostgreSQL reference implementation of
// api.StoreProvider. It targets PG 13+ (the floor for SKIP LOCKED + the
// uniform `ON CONFLICT … DO UPDATE` syntax we rely on). Multi-process
// concurrent writers are fully supported.
//
// Driver: github.com/jackc/pgx/v5/stdlib, registered as "pgx". Pass a DSN
// in libpq form ("postgres://user:pass@host:5432/db?sslmode=disable") via
// Options.DSN, or hand in an externally-managed *sql.DB through
// NewProviderWithDB if you want to share a pool with other components.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Reference: postgres".
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/storage/sqlbase"

	// pgx/v5/stdlib registers the "pgx" database/sql driver via init().
	// dialect.go imports pgconn (a separate sub-package) for typed error
	// inspection, so this blank import is required to wire up the driver.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string

//go:embed schema.sql
var migrationsFS embed.FS

// Options controls Provider construction. DSN is required when calling
// NewProvider — there is no sensible default for Postgres.
type Options struct {
	DSN         string
	AutoMigrate bool
}

// MigrationsFS exposes the embedded schema.sql for callers that manage
// their own migration tool.
func MigrationsFS() fs.FS { return migrationsFS }

// Provider is the Postgres api.StoreProvider implementation. It satisfies
// api.StoreProvider, api.CapabilityReporter, and api.ProviderCloser.
type Provider struct {
	db *sql.DB
}

// NewProvider opens a Postgres pool against opts.DSN. AutoMigrate runs
// the embedded schema on Open; turn it off if a separate migration tool
// owns the schema.
func NewProvider(opts Options) (*Provider, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}
	db, err := sql.Open("pgx", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	p := &Provider{db: db}
	if opts.AutoMigrate {
		if err := p.migrate(context.Background()); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return p, nil
}

// NewProviderWithDB wraps an externally-owned *sql.DB. The caller retains
// ownership; calling Close on this Provider closes the pool, which may
// surprise the owner — wrap or skip Close in that case.
func NewProviderWithDB(db *sql.DB) *Provider {
	return &Provider{db: db}
}

func (p *Provider) migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	return nil
}

// Begin opens a new transactional unit of work. Default isolation
// (READ COMMITTED) is sufficient for the contract's CAS-on-version
// semantics; serializable isolation is left to callers via DB().
func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("postgres: begin: %w", err)
	}
	return sqlbase.NewUnitOfWork(tx, postgresDialect{}), nil
}

// Capabilities self-declares the Postgres reference impl's feature set.
// Concurrent writers yes; blackboard subscribe is deferred to v0.8.1
// (LISTEN/NOTIFY integration is out-of-scope for the initial release).
func (p *Provider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return api.StoreCapabilities{
		SupportsTransactions:        true,
		SupportsBlackboardSubscribe: false,
		SupportsListPending:         true,
		SupportsConcurrentWriters:   true,
		SupportsDeadLetterRequeue:   true,
	}, nil
}

// Close closes the underlying *sql.DB.
func (p *Provider) Close(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("postgres: close: %w", err)
	}
	return nil
}

// DB exposes the underlying *sql.DB for advanced callers (e.g. issuing
// LISTEN/NOTIFY traffic outside the contract surface).
func (p *Provider) DB() *sql.DB { return p.db }

// ErrTxClosed is returned by store methods after Commit/Rollback. Aliased
// to sqlbase.ErrTxClosed so callers can errors.Is against either symbol.
var ErrTxClosed = sqlbase.ErrTxClosed
