package dialects

import (
	"fmt"

	"github.com/pressly/goose/v3/database/dialect"
)

// NewSpanner returns a [dialect.Querier] for Spanner dialect.
func NewSpanner() dialect.Querier {
	return &spanner{}
}

type spanner struct{}

var _ dialect.Querier = (*spanner)(nil)

func (s *spanner) CreateTable(tableName string) string {
	q := `CREATE TABLE %s (
		version_id INT64 NOT NULL,
		is_applied BOOL NOT NULL,
		tstamp TIMESTAMP DEFAULT (CURRENT_TIMESTAMP()),
	) PRIMARY KEY(version_id)`
	return fmt.Sprintf(q, tableName)
}

func (s *spanner) InsertVersion(tableName string) string {
	q := `INSERT INTO %s (version_id, is_applied) VALUES (?, ?)`
	return fmt.Sprintf(q, tableName)
}

func (s *spanner) DeleteVersion(tableName string) string {
	q := `DELETE FROM %s WHERE version_id=?`
	return fmt.Sprintf(q, tableName)
}

func (s *spanner) GetMigrationByVersion(tableName string) string {
	q := `SELECT tstamp, is_applied FROM %s WHERE version_id=? ORDER BY tstamp DESC LIMIT 1`
	return fmt.Sprintf(q, tableName)
}

func (s *spanner) ListMigrations(tableName string) string {
	q := `SELECT version_id, is_applied from %s ORDER BY version_id DESC`
	return fmt.Sprintf(q, tableName)
}

func (s *spanner) GetLatestVersion(tableName string) string {
	q := `SELECT MAX(version_id) FROM %s`
	return fmt.Sprintf(q, tableName)
}
