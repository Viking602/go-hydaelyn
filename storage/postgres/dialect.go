package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
)

// postgresDialect implements sqlbase.Dialect for PostgreSQL. Postgres uses
// numbered `$N` placeholders (sqlbase rewrites `?` accordingly) and the
// SQL-standard `ON CONFLICT(pk) DO UPDATE SET col=excluded.col` upsert.
type postgresDialect struct{}

func (postgresDialect) Name() string           { return "postgres" }
func (postgresDialect) Rebind(q string) string { return sqlbase.RebindDollar(q) }
func (postgresDialect) UpsertClause(pk, updateCols []string) string {
	return sqlbase.UpsertOnConflict(pk, updateCols)
}

// IsDuplicateKey recognizes Postgres SQLSTATE 23505 (unique_violation).
func (postgresDialect) IsDuplicateKey(err error) bool {
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) {
		return false
	}
	return pgerr.Code == "23505"
}
