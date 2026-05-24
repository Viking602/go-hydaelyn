package mysql

import "github.com/Viking602/go-hydaelyn/storage/sqlbase"

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
