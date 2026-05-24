// Package sqlbase holds the shared database/sql implementation of every
// api.* store used by the v0.8.0 reference SQL providers (sqlite, mysql,
// postgres). The package is intentionally framework-internal in spirit:
// external adapter authors are NOT expected to import it — they should
// implement the api.* interfaces directly against their own data layer
// (see ADR-012 / Layer 3). sqlbase exists so the three reference
// providers do not duplicate 1000 lines of CRUD between themselves.
//
// All cross-dialect differences flow through the Dialect interface:
// placeholder syntax (`?` vs `$N`), upsert syntax (`ON CONFLICT … DO
// UPDATE` vs `ON DUPLICATE KEY UPDATE`), and (when needed) lock hints
// like `FOR UPDATE SKIP LOCKED`.
package sqlbase

import (
	"strconv"
	"strings"
)

// Dialect captures the small set of SQL differences between the three
// supported reference databases. Implementations live alongside the
// individual providers (sqlite/dialect.go, postgres/dialect.go,
// mysql/dialect.go) so that sqlbase stays driver-free.
type Dialect interface {
	// Name returns a short identifier ("sqlite", "postgres", "mysql")
	// used in error wrapping.
	Name() string

	// Rebind converts a `?`-style placeholder query into the syntax this
	// dialect expects. SQLite + MySQL return q unchanged; Postgres
	// rewrites each `?` into `$1`, `$2`, … in order.
	Rebind(query string) string

	// UpsertClause returns the trailing clause that turns an INSERT into
	// an idempotent upsert against the given primary-key columns,
	// updating updateCols on conflict. The leading INSERT is the
	// caller's responsibility.
	//
	// SQLite/Postgres: "ON CONFLICT(pk1,pk2) DO UPDATE SET c=excluded.c"
	// MySQL:           "ON DUPLICATE KEY UPDATE c=VALUES(c)"
	UpsertClause(pk []string, updateCols []string) string
}

// RebindDollar rewrites `?` placeholders into `$N` form, used by the
// Postgres dialect.
func RebindDollar(q string) string {
	var out strings.Builder
	out.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c != '?' {
			out.WriteByte(c)
			continue
		}
		n++
		out.WriteByte('$')
		out.WriteString(strconv.Itoa(n))
	}
	return out.String()
}

// UpsertOnConflict builds the SQLite/Postgres `ON CONFLICT … DO UPDATE`
// clause. Exposed so dialects can compose it without re-importing
// strings everywhere.
func UpsertOnConflict(pk, updateCols []string) string {
	sets := make([]string, len(updateCols))
	for i, c := range updateCols {
		sets[i] = c + "=excluded." + c
	}
	return "ON CONFLICT(" + strings.Join(pk, ",") + ") DO UPDATE SET " + strings.Join(sets, ",")
}

// UpsertOnDuplicateKey builds the MySQL `ON DUPLICATE KEY UPDATE` clause.
func UpsertOnDuplicateKey(_, updateCols []string) string {
	sets := make([]string, len(updateCols))
	for i, c := range updateCols {
		sets[i] = c + "=VALUES(" + c + ")"
	}
	return "ON DUPLICATE KEY UPDATE " + strings.Join(sets, ",")
}

// Placeholders returns a comma-joined run of n `?` placeholders for use
// inside IN(…) clauses. The result is fed through Dialect.Rebind by
// callers, so this stays placeholder-agnostic.
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
