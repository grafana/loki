package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3/internal/dialect/dialectquery"
)

// Dialect is the type of database dialect.
type Dialect string

const (
	DialectClickHouse Dialect = "clickhouse"
	DialectMSSQL      Dialect = "mssql"
	DialectMySQL      Dialect = "mysql"
	DialectPostgres   Dialect = "postgres"
	DialectRedshift   Dialect = "redshift"
	DialectSQLite3    Dialect = "sqlite3"
	DialectTiDB       Dialect = "tidb"
	DialectTurso      Dialect = "turso"
	DialectVertica    Dialect = "vertica"
	DialectYdB        Dialect = "ydb"
	DialectStarrocks  Dialect = "starrocks"
)

// NewStore returns a new [Store] implementation for the given dialect.
func NewStore(dialect Dialect, tablename string) (Store, error) {
	if tablename == "" {
		return nil, errors.New("table name must not be empty")
	}
	if dialect == "" {
		return nil, errors.New("dialect must not be empty")
	}
	lookup := map[Dialect]dialectquery.Querier{
		DialectClickHouse: &dialectquery.Clickhouse{},
		DialectMSSQL:      &dialectquery.Sqlserver{},
		DialectMySQL:      &dialectquery.Mysql{},
		DialectPostgres:   &dialectquery.Postgres{},
		DialectRedshift:   &dialectquery.Redshift{},
		DialectSQLite3:    &dialectquery.Sqlite3{},
		DialectTiDB:       &dialectquery.Tidb{},
		DialectVertica:    &dialectquery.Vertica{},
		DialectYdB:        &dialectquery.Ydb{},
		DialectTurso:      &dialectquery.Turso{},
		DialectStarrocks:  &dialectquery.Starrocks{},
	}
	querier, ok := lookup[dialect]
	if !ok {
		return nil, fmt.Errorf("unknown dialect: %q", dialect)
	}
	return &store{
		tablename: tablename,
		querier:   dialectquery.NewQueryController(querier),
	}, nil
}

type store struct {
	tablename string
	querier   *dialectquery.QueryController
}

var _ Store = (*store)(nil)

func (s *store) Tablename() string {
	return s.tablename
}

func (s *store) CreateVersionTable(ctx context.Context, db DBTxConn) error {
	q := s.querier.CreateTable(s.tablename)
	if _, err := db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("failed to create version table %q: %w", s.tablename, err)
	}
	return nil
}

func (s *store) Insert(ctx context.Context, db DBTxConn, req InsertRequest) error {
	q := s.querier.InsertVersion(s.tablename)
	if _, err := db.ExecContext(ctx, q, req.Version, true); err != nil {
		return fmt.Errorf("failed to insert version %d: %w", req.Version, err)
	}
	return nil
}

func (s *store) Delete(ctx context.Context, db DBTxConn, version int64) error {
	q := s.querier.DeleteVersion(s.tablename)
	if _, err := db.ExecContext(ctx, q, version); err != nil {
		return fmt.Errorf("failed to delete version %d: %w", version, err)
	}
	return nil
}

func (s *store) GetMigration(
	ctx context.Context,
	db DBTxConn,
	version int64,
) (*GetMigrationResult, error) {
	q := s.querier.GetMigrationByVersion(s.tablename)
	var result GetMigrationResult
	if err := db.QueryRowContext(ctx, q, version).Scan(
		&result.Timestamp,
		&result.IsApplied,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %d", ErrVersionNotFound, version)
		}
		return nil, fmt.Errorf("failed to get migration %d: %w", version, err)
	}
	return &result, nil
}

func (s *store) GetLatestVersion(ctx context.Context, db DBTxConn) (int64, error) {
	q := s.querier.GetLatestVersion(s.tablename)
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, q).Scan(&version); err != nil {
		return -1, fmt.Errorf("failed to get latest version: %w", err)
	}
	if !version.Valid {
		return -1, fmt.Errorf("latest %w", ErrVersionNotFound)
	}
	return version.Int64, nil
}

func (s *store) ListMigrations(
	ctx context.Context,
	db DBTxConn,
) ([]*ListMigrationsResult, error) {
	q := s.querier.ListMigrations(s.tablename)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to list migrations: %w", err)
	}
	defer rows.Close()

	var migrations []*ListMigrationsResult
	for rows.Next() {
		var result ListMigrationsResult
		if err := rows.Scan(&result.Version, &result.IsApplied); err != nil {
			return nil, fmt.Errorf("failed to scan list migrations result: %w", err)
		}
		migrations = append(migrations, &result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return migrations, nil
}

//
//
//
// Additional methods that are not part of the core Store interface, but are extended by the
// [controller.StoreController] type.
//
//
//

func (s *store) TableExists(ctx context.Context, db DBTxConn) (bool, error) {
	q := s.querier.TableExists(s.tablename)
	if q == "" {
		return false, errors.ErrUnsupported
	}
	var exists bool
	// Note, we do not pass the table name as an argument to the query, as the query should be
	// pre-defined by the dialect.
	if err := db.QueryRowContext(ctx, q).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check if table exists: %w", err)
	}
	return exists, nil
}
