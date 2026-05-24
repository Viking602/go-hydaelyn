// Package sqlite is the v0.8.0 SQLite reference implementation of
// api.StoreProvider. It targets single-node deployments and local-first
// apps; multi-process concurrent writers are not supported (SQLite's
// write lock serializes them).
//
// Driver: modernc.org/sqlite (pure Go, no CGO). The driver is registered
// via blank import; callers can pass any *sql.DB into NewProviderWithDB
// if they want to substitute the mattn/go-sqlite3 build.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Reference: sqlite".
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
	// modernc.org/sqlite is imported (typed) from dialect.go for its
	// error-type and registers the "sqlite" driver via init() — no blank
	// import needed here.
)

//go:embed schema.sql
var schemaSQL string

//go:embed schema.sql
var migrationsFS embed.FS

// Options controls Provider construction.
type Options struct {
	// DSN is the SQLite data source name. Defaults to ":memory:" — useful
	// for tests, lost on process exit.
	DSN string
	// AutoMigrate runs the embedded schema.sql on Open. When false, the
	// caller is responsible for running the SQL from MigrationsFS().
	AutoMigrate bool
}

// MigrationsFS exposes the embedded schema.sql for callers that manage
// their own migration tool. Today the SQLite reference ships one file —
// future versions may add numbered migrations to this FS.
func MigrationsFS() fs.FS { return migrationsFS }

// Provider is the SQLite api.StoreProvider implementation. It satisfies
// api.StoreProvider, api.CapabilityReporter, and api.ProviderCloser.
type Provider struct {
	db *sql.DB
}

// NewProvider opens a SQLite database at opts.DSN. When opts.AutoMigrate
// is true the schema is applied automatically.
func NewProvider(opts Options) (*Provider, error) {
	dsn := opts.DSN
	if dsn == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", dsn, err)
	}
	// SQLite's write lock means only one writer at a time; cap pool at 1
	// to avoid "database is locked" under contention from our own pool.
	db.SetMaxOpenConns(1)
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
// ownership of the DB (Close on the Provider is a no-op for the DB).
func NewProviderWithDB(db *sql.DB) *Provider {
	return &Provider{db: db}
}

func (p *Provider) migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("sqlite: migrate: %w", err)
	}
	return nil
}

// Begin opens a new transactional unit of work. SQLite uses BEGIN
// IMMEDIATE to acquire the reserved lock up front, avoiding mid-write
// SQLITE_BUSY errors on concurrent UoWs.
func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite: begin: %w", err)
	}
	return newUnitOfWork(tx), nil
}

// Capabilities self-declares the SQLite reference impl's optional feature
// set. Transactions yes, list-pending yes, dead-letter requeue yes —
// concurrent writers and blackboard subscribe are NOT supported.
func (p *Provider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return api.StoreCapabilities{
		SupportsTransactions:        true,
		SupportsBlackboardSubscribe: false,
		SupportsListPending:         true,
		SupportsConcurrentWriters:   false,
		SupportsDeadLetterRequeue:   true,
	}, nil
}

// Close closes the underlying *sql.DB. NewProviderWithDB callers should
// close their own DB instead; calling Close here is harmless but
// double-closes the pool.
func (p *Provider) Close(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	return nil
}

// DB exposes the underlying *sql.DB for advanced callers (e.g. running
// custom queries inside the same connection pool). Most users should not
// need this.
func (p *Provider) DB() *sql.DB { return p.db }

// ErrTxClosed is returned by store methods after Commit/Rollback. The
// shared sqlbase package owns the sentinel; we re-export it so callers
// importing storage/sqlite can use errors.Is without a second import.
var ErrTxClosed = sqlbase.ErrTxClosed
