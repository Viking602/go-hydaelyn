// Package mysql is the v0.8.0 MySQL reference implementation of
// api.StoreProvider. The schema and queries target the lowest common
// denominator across MySQL 8.0+, MariaDB 10.5+, TiDB 6+, and OceanBase
// 4.x in MySQL compatibility mode — that is, no JSON_TABLE, no
// JSON_OVERLAPS, no AUTO_INCREMENT primary keys, no generated columns.
//
// Driver: github.com/go-sql-driver/mysql, registered as "mysql". DSN
// follows the driver's standard form, e.g.
// "user:pass@tcp(localhost:3306)/hydaelyn?parseTime=true". `parseTime=true`
// is recommended but not required — payload columns hold full timestamps.
//
// Spec anchor: docs/product-spec/v0.8.0/05-storage.md §"Reference: mysql".
package mysql

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
	// github.com/go-sql-driver/mysql is imported (typed) from dialect.go for
	// MySQLError and registers the "mysql" driver via init() — no blank
	// import needed here.
)

//go:embed schema.sql
var schemaSQL string

//go:embed schema.sql
var migrationsFS embed.FS

// Options controls Provider construction. DSN is required.
type Options struct {
	DSN         string
	AutoMigrate bool
}

// MigrationsFS exposes the embedded schema.sql for callers that manage
// their own migration tool.
func MigrationsFS() fs.FS { return migrationsFS }

// Provider is the MySQL api.StoreProvider implementation. It satisfies
// api.StoreProvider, api.CapabilityReporter, and api.ProviderCloser.
type Provider struct {
	db *sql.DB
}

// NewProvider opens a MySQL pool against opts.DSN. AutoMigrate runs the
// embedded schema on Open by splitting statements on `;` — the schema
// avoids stored procedures and triggers so naive splitting is safe.
func NewProvider(opts Options) (*Provider, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("mysql: DSN is required")
	}
	db, err := sql.Open("mysql", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("mysql: open: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql: ping: %w", err)
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

// NewProviderWithDB wraps an externally-owned *sql.DB.
func NewProviderWithDB(db *sql.DB) *Provider {
	return &Provider{db: db}
}

// migrate applies the embedded schema. The MySQL driver does not support
// multi-statement queries by default, so we split on `;` and execute the
// non-empty pieces in order. The schema is single-line per statement and
// contains no string literals with semicolons, so this is safe.
func (p *Provider) migrate(ctx context.Context) error {
	for _, stmt := range strings.Split(schemaSQL, ";") {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		if _, err := p.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("mysql: migrate: %w", err)
		}
	}
	return nil
}

// Begin opens a new transactional unit of work. Default isolation
// (REPEATABLE READ on MySQL/MariaDB, configurable on TiDB) is sufficient;
// the contract's CAS-on-version semantics do not require SERIALIZABLE.
func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mysql: begin: %w", err)
	}
	return sqlbase.NewUnitOfWork(tx, mysqlDialect{}), nil
}

// Capabilities self-declares the MySQL reference impl's feature set.
// Concurrent writers yes; blackboard subscribe stays false (no built-in
// pub/sub channel — callers should layer NATS/Redis on top if needed).
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
		return fmt.Errorf("mysql: close: %w", err)
	}
	return nil
}

// DB exposes the underlying *sql.DB for advanced callers.
func (p *Provider) DB() *sql.DB { return p.db }

// ErrTxClosed is returned by store methods after Commit/Rollback. Aliased
// to sqlbase.ErrTxClosed so callers can errors.Is against either symbol.
var ErrTxClosed = sqlbase.ErrTxClosed
