package dialectquery

import "fmt"

type Redshift struct{}

var _ Querier = (*Redshift)(nil)

func (r *Redshift) CreateTable(tableName string) string {
	q := `CREATE TABLE %s (
		id integer NOT NULL identity(1, 1),
		version_id bigint NOT NULL,
		is_applied boolean NOT NULL,
		tstamp timestamp NULL default sysdate,
		PRIMARY KEY(id)
	)`
	return fmt.Sprintf(q, tableName)
}

func (r *Redshift) InsertVersion(tableName string) string {
	q := `INSERT INTO %s (version_id, is_applied) VALUES ($1, $2)`
	return fmt.Sprintf(q, tableName)
}

func (r *Redshift) DeleteVersion(tableName string) string {
	q := `DELETE FROM %s WHERE version_id=$1`
	return fmt.Sprintf(q, tableName)
}

func (r *Redshift) GetMigrationByVersion(tableName string) string {
	q := `SELECT tstamp, is_applied FROM %s WHERE version_id=$1 ORDER BY tstamp DESC LIMIT 1`
	return fmt.Sprintf(q, tableName)
}

func (r *Redshift) ListMigrations(tableName string) string {
	q := `SELECT version_id, is_applied from %s ORDER BY id DESC`
	return fmt.Sprintf(q, tableName)
}

func (r *Redshift) GetLatestVersion(tableName string) string {
	q := `SELECT max(version_id) FROM %s`
	return fmt.Sprintf(q, tableName)
}
