package sqlite

import (
	"database/sql"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
)

// newUnitOfWork wraps a *sql.Tx in the shared sqlbase implementation
// using the SQLite dialect. All 15 api.* stores are provided by sqlbase;
// the SQLite reference impl carries no per-store logic of its own.
func newUnitOfWork(tx *sql.Tx) api.UnitOfWork {
	return sqlbase.NewUnitOfWork(tx, sqliteDialect{})
}
