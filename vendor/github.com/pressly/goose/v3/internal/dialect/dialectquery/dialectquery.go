package dialectquery

import "strings"

// Querier is the interface that wraps the basic methods to create a dialect specific query.
type Querier interface {
	// CreateTable returns the SQL query string to create the db version table.
	CreateTable(tableName string) string

	// InsertVersion returns the SQL query string to insert a new version into the db version table.
	InsertVersion(tableName string) string

	// DeleteVersion returns the SQL query string to delete a version from the db version table.
	DeleteVersion(tableName string) string

	// GetMigrationByVersion returns the SQL query string to get a single migration by version.
	//
	// The query should return the timestamp and is_applied columns.
	GetMigrationByVersion(tableName string) string

	// ListMigrations returns the SQL query string to list all migrations in descending order by id.
	//
	// The query should return the version_id and is_applied columns.
	ListMigrations(tableName string) string

	// GetLatestVersion returns the SQL query string to get the last version_id from the db version
	// table. Returns a nullable int64 value.
	GetLatestVersion(tableName string) string
}

var _ Querier = (*QueryController)(nil)

type QueryController struct{ Querier }

// NewQueryController returns a new QueryController that wraps the given Querier.
func NewQueryController(querier Querier) *QueryController {
	return &QueryController{Querier: querier}
}

// Optional methods

// TableExists returns the SQL query string to check if the version table exists. If the Querier
// does not implement this method, it will return an empty string.
//
// Returns a boolean value.
func (c *QueryController) TableExists(tableName string) string {
	if t, ok := c.Querier.(interface{ TableExists(string) string }); ok {
		return t.TableExists(tableName)
	}
	return ""
}

func parseTableIdentifier(name string) (schema, table string) {
	schema, table, found := strings.Cut(name, ".")
	if !found {
		return "", name
	}
	return schema, table
}
