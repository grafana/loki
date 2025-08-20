package goose

import (
	"fmt"

	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/internal/dialect"
)

// Dialect is the type of database dialect. It is an alias for [database.Dialect].
type Dialect = database.Dialect

const (
	DialectClickHouse Dialect = database.DialectClickHouse
	DialectMSSQL      Dialect = database.DialectMSSQL
	DialectMySQL      Dialect = database.DialectMySQL
	DialectPostgres   Dialect = database.DialectPostgres
	DialectRedshift   Dialect = database.DialectRedshift
	DialectSQLite3    Dialect = database.DialectSQLite3
	DialectTiDB       Dialect = database.DialectTiDB
	DialectVertica    Dialect = database.DialectVertica
	DialectYdB        Dialect = database.DialectYdB
	DialectStarrocks  Dialect = database.DialectStarrocks
)

func init() {
	store, _ = dialect.NewStore(dialect.Postgres)
}

var store dialect.Store

// SetDialect sets the dialect to use for the goose package.
func SetDialect(s string) error {
	var d dialect.Dialect
	switch s {
	case "postgres", "pgx":
		d = dialect.Postgres
	case "mysql":
		d = dialect.Mysql
	case "sqlite3", "sqlite":
		d = dialect.Sqlite3
	case "mssql", "azuresql", "sqlserver":
		d = dialect.Sqlserver
	case "redshift":
		d = dialect.Redshift
	case "tidb":
		d = dialect.Tidb
	case "clickhouse":
		d = dialect.Clickhouse
	case "vertica":
		d = dialect.Vertica
	case "ydb":
		d = dialect.Ydb
	case "turso":
		d = dialect.Turso
	case "starrocks":
		d = dialect.Starrocks
	default:
		return fmt.Errorf("%q: unknown dialect", s)
	}
	var err error
	store, err = dialect.NewStore(d)
	return err
}
