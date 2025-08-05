package dialectquery

type Turso struct {
	Sqlite3
}

var _ Querier = (*Turso)(nil)
