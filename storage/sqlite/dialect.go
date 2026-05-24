package sqlite

import "github.com/Viking602/go-hydaelyn/storage/sqlbase"

// sqliteDialect implements sqlbase.Dialect for SQLite. SQLite uses the
// same `?` placeholder syntax sqlbase emits internally, and supports
// `ON CONFLICT(pk) DO UPDATE SET col=excluded.col` upserts since 3.24.
type sqliteDialect struct{}

func (sqliteDialect) Name() string           { return "sqlite" }
func (sqliteDialect) Rebind(q string) string { return q }
func (sqliteDialect) UpsertClause(pk, updateCols []string) string {
	return sqlbase.UpsertOnConflict(pk, updateCols)
}
