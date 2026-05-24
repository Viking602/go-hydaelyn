package sqlite

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
)

// sqliteDialect implements sqlbase.Dialect for SQLite. SQLite uses the
// same `?` placeholder syntax sqlbase emits internally, and supports
// `ON CONFLICT(pk) DO UPDATE SET col=excluded.col` upserts since 3.24.
type sqliteDialect struct{}

func (sqliteDialect) Name() string           { return "sqlite" }
func (sqliteDialect) Rebind(q string) string { return q }
func (sqliteDialect) UpsertClause(pk, updateCols []string) string {
	return sqlbase.UpsertOnConflict(pk, updateCols)
}

// IsDuplicateKey recognizes SQLITE_CONSTRAINT_UNIQUE and
// SQLITE_CONSTRAINT_PRIMARYKEY — both fire when a concurrent insert loses
// the CAS race for an existing row.
func (sqliteDialect) IsDuplicateKey(err error) bool {
	var serr *sqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	code := serr.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
