package mysql

import (
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/Viking602/go-hydaelyn/storage/sqlbase"
)

// mysqlDialect implements sqlbase.Dialect for MySQL/MariaDB/TiDB/OceanBase
// (MySQL mode). All four use `?` placeholders and the legacy
// `ON DUPLICATE KEY UPDATE col=VALUES(col)` upsert; that form survives
// MySQL 8.0's deprecation warning and remains the only upsert syntax
// supported across TiDB and OceanBase.
type mysqlDialect struct{}

func (mysqlDialect) Name() string           { return "mysql" }
func (mysqlDialect) Rebind(q string) string { return q }
func (mysqlDialect) UpsertClause(pk, updateCols []string) string {
	return sqlbase.UpsertOnDuplicateKey(pk, updateCols)
}

// IsDuplicateKey recognizes MySQL error 1062 (ER_DUP_ENTRY).
func (mysqlDialect) IsDuplicateKey(err error) bool {
	var merr *mysql.MySQLError
	if !errors.As(err, &merr) {
		return false
	}
	return merr.Number == 1062
}
