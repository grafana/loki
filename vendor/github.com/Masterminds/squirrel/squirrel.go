// Package squirrel provides a fluent SQL generator.
//
// See https://github.com/lann/squirrel for examples.
package squirrel

import (
	"bytes"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lann/builder"
)

// Sqlizer is the interface that wraps the ToSql method.
//
// ToSql returns a SQL representation of the Sqlizer, along with a slice of args
// as passed to e.g. database/sql.Exec. It can also return an error.
type Sqlizer interface {
	ToSql() (string, []interface{}, error)
}

// Execer is the interface that wraps the Exec method.
//
// Exec executes the given query as implemented by database/sql.Exec.
type Execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// Queryer is the interface that wraps the Query method.
//
// Query executes the given query as implemented by database/sql.Query.
type Queryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// QueryRower is the interface that wraps the QueryRow method.
//
// QueryRow executes the given query as implemented by database/sql.QueryRow.
type QueryRower interface {
	QueryRow(query string, args ...interface{}) RowScanner
}

// BaseRunner groups the Execer and Queryer interfaces.
type BaseRunner interface {
	Execer
	Queryer
}

// Runner groups the Execer, Queryer, and QueryRower interfaces.
type Runner interface {
	Execer
	Queryer
	QueryRower
}

// DBRunner wraps sql.DB to implement Runner.
type dbRunner struct {
	*sql.DB
}

func (r *dbRunner) QueryRow(query string, args ...interface{}) RowScanner {
	return r.DB.QueryRow(query, args...)
}

type txRunner struct {
	*sql.Tx
}

func (r *txRunner) QueryRow(query string, args ...interface{}) RowScanner {
	return r.Tx.QueryRow(query, args...)
}

func setRunWith(b interface{}, baseRunner BaseRunner) interface{} {
	var runner Runner
	switch r := baseRunner.(type) {
	case Runner:
		runner = r
	case *sql.DB:
		runner = &dbRunner{r}
	case *sql.Tx:
		runner = &txRunner{r}
	}
	return builder.Set(b, "RunWith", runner)
}

// RunnerNotSet is returned by methods that need a Runner if it isn't set.
var RunnerNotSet = fmt.Errorf("cannot run; no Runner set (RunWith)")

// RunnerNotQueryRunner is returned by QueryRow if the RunWith value doesn't implement QueryRower.
var RunnerNotQueryRunner = fmt.Errorf("cannot QueryRow; Runner is not a QueryRower")

// ExecWith Execs the SQL returned by s with db.
func ExecWith(db Execer, s Sqlizer) (res sql.Result, err error) {
	query, args, err := s.ToSql()
	if err != nil {
		return
	}
	return db.Exec(query, args...)
}

// QueryWith Querys the SQL returned by s with db.
func QueryWith(db Queryer, s Sqlizer) (rows *sql.Rows, err error) {
	query, args, err := s.ToSql()
	if err != nil {
		return
	}
	return db.Query(query, args...)
}

// QueryRowWith QueryRows the SQL returned by s with db.
func QueryRowWith(db QueryRower, s Sqlizer) RowScanner {
	query, args, err := s.ToSql()
	return &Row{RowScanner: db.QueryRow(query, args...), err: err}
}

// DebugSqlizer calls ToSql on s and shows the approximate SQL to be executed
//
// If ToSql returns an error, the result of this method will look like:
// "[ToSql error: %s]" or "[DebugSqlizer error: %s]"
//
// IMPORTANT: As its name suggests, this function should only be used for
// debugging. While the string result *might* be valid SQL, this function does
// not try very hard to ensure it. Additionally, executing the output of this
// function with any untrusted user input is certainly insecure.
func DebugSqlizer(s Sqlizer) string {
	sql, args, err := s.ToSql()
	if err != nil {
		return fmt.Sprintf("[ToSql error: %s]", err)
	}

	// TODO: dedupe this with placeholder.go
	buf := &bytes.Buffer{}
	i := 0
	for {
		p := strings.Index(sql, "?")
		if p == -1 {
			break
		}
		if len(sql[p:]) > 1 && sql[p:p+2] == "??" { // escape ?? => ?
			buf.WriteString(sql[:p])
			buf.WriteString("?")
			if len(sql[p:]) == 1 {
				break
			}
			sql = sql[p+2:]
		} else {
			if i+1 > len(args) {
				return fmt.Sprintf(
					"[DebugSqlizer error: too many placeholders in %#v for %d args]",
					sql, len(args))
			}
			buf.WriteString(sql[:p])
			fmt.Fprintf(buf, "'%v'", args[i])
			sql = sql[p+1:]
			i++
		}
	}
	if i < len(args) {
		return fmt.Sprintf(
			"[DebugSqlizer error: not enough placeholders in %#v for %d args]",
			sql, len(args))
	}
	buf.WriteString(sql)
	return buf.String()
}
