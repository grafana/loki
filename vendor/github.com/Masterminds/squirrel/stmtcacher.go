package squirrel

import (
	"database/sql"
	"sync"
)

// Prepareer is the interface that wraps the Prepare method.
//
// Prepare executes the given query as implemented by database/sql.Prepare.
type Preparer interface {
	Prepare(query string) (*sql.Stmt, error)
}

// DBProxy groups the Execer, Queryer, QueryRower, and Preparer interfaces.
type DBProxy interface {
	Execer
	Queryer
	QueryRower
	Preparer
}

type stmtCacher struct {
	prep  Preparer
	cache map[string]*sql.Stmt
	mu    sync.Mutex
}

// NewStmtCacher returns a DBProxy wrapping prep that caches Prepared Stmts.
//
// Stmts are cached based on the string value of their queries.
func NewStmtCacher(prep Preparer) DBProxy {
	return &stmtCacher{prep: prep, cache: make(map[string]*sql.Stmt)}
}

func (sc *stmtCacher) Prepare(query string) (*sql.Stmt, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	stmt, ok := sc.cache[query]
	if ok {
		return stmt, nil
	}
	stmt, err := sc.prep.Prepare(query)
	if err == nil {
		sc.cache[query] = stmt
	}
	return stmt, err
}

func (sc *stmtCacher) Exec(query string, args ...interface{}) (res sql.Result, err error) {
	stmt, err := sc.Prepare(query)
	if err != nil {
		return
	}
	return stmt.Exec(args...)
}

func (sc *stmtCacher) Query(query string, args ...interface{}) (rows *sql.Rows, err error) {
	stmt, err := sc.Prepare(query)
	if err != nil {
		return
	}
	return stmt.Query(args...)
}

func (sc *stmtCacher) QueryRow(query string, args ...interface{}) RowScanner {
	stmt, err := sc.Prepare(query)
	if err != nil {
		return &Row{err: err}
	}
	return stmt.QueryRow(args...)
}

type DBProxyBeginner interface {
	DBProxy
	Begin() (*sql.Tx, error)
}

type stmtCacheProxy struct {
	DBProxy
	db *sql.DB
}

func NewStmtCacheProxy(db *sql.DB) DBProxyBeginner {
	return &stmtCacheProxy{DBProxy: NewStmtCacher(db), db: db}
}

func (sp *stmtCacheProxy) Begin() (*sql.Tx, error) {
	return sp.db.Begin()
}
