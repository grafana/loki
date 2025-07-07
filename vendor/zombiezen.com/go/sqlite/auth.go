// Copyright 2021 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"
	"strings"
	"sync"

	"modernc.org/libc"
	lib "modernc.org/sqlite/lib"
)

// An Authorizer is called during statement preparation to see whether an action
// is allowed by the application. An Authorizer must not modify the database
// connection, including by preparing statements.
//
// See https://sqlite.org/c3ref/set_authorizer.html for a longer explanation.
type Authorizer interface {
	Authorize(Action) AuthResult
}

// SetAuthorizer registers an authorizer for the database connection.
// SetAuthorizer(nil) clears any authorizer previously set.
func (c *Conn) SetAuthorizer(auth Authorizer) error {
	if c == nil {
		return fmt.Errorf("sqlite: set authorizer: nil connection")
	}
	if auth == nil {
		c.releaseAuthorizer()
		res := ResultCode(lib.Xsqlite3_set_authorizer(c.tls, c.conn, 0, 0))
		if err := res.ToError(); err != nil {
			return fmt.Errorf("sqlite: set authorizer: %w", err)
		}
		return nil
	}

	authorizers.mu.Lock()
	if authorizers.m == nil {
		authorizers.m = make(map[uintptr]Authorizer)
	}
	authorizers.m[c.conn] = auth
	authorizers.mu.Unlock()

	xAuth := cFuncPointer(authTrampoline)

	res := ResultCode(lib.Xsqlite3_set_authorizer(c.tls, c.conn, xAuth, c.conn))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: set authorizer: %w", err)
	}
	return nil
}

func (c *Conn) releaseAuthorizer() {
	authorizers.mu.Lock()
	delete(authorizers.m, c.conn)
	authorizers.mu.Unlock()
}

var authorizers struct {
	mu sync.RWMutex
	m  map[uintptr]Authorizer // sqlite3* -> Authorizer
}

func authTrampoline(tls *libc.TLS, conn uintptr, op int32, cArg1, cArg2, cDB, cTrigger uintptr) int32 {
	authorizers.mu.RLock()
	auth := authorizers.m[conn]
	authorizers.mu.RUnlock()
	return int32(auth.Authorize(Action{
		op:       OpType(op),
		arg1:     libc.GoString(cArg1),
		arg2:     libc.GoString(cArg2),
		database: libc.GoString(cDB),
		trigger:  libc.GoString(cTrigger),
	}))
}

// AuthorizeFunc is a function that implements Authorizer.
type AuthorizeFunc func(Action) AuthResult

// Authorize calls f.
func (f AuthorizeFunc) Authorize(action Action) AuthResult {
	return f(action)
}

// AuthResult is the result of a call to an Authorizer. The zero value is
// AuthResultOK.
type AuthResult int32

// Possible return values from Authorize.
const (
	// AuthResultOK allows the SQL statement to be compiled.
	AuthResultOK AuthResult = lib.SQLITE_OK
	// AuthResultDeny causes the entire SQL statement to be rejected with an error.
	AuthResultDeny AuthResult = lib.SQLITE_DENY
	// AuthResultIgnore disallows the specific action but allow the SQL statement
	// to continue to be compiled. For OpRead, this substitutes a NULL for the
	// column value. For OpDelete, the DELETE operation proceeds but the truncate
	// optimization is disabled and all rows are deleted individually.
	AuthResultIgnore AuthResult = lib.SQLITE_IGNORE
)

// String returns the C constant name of the result.
func (result AuthResult) String() string {
	switch result {
	case AuthResultOK:
		return "SQLITE_OK"
	case AuthResultDeny:
		return "SQLITE_DENY"
	case AuthResultIgnore:
		return "SQLITE_IGNORE"
	default:
		return fmt.Sprintf("AuthResult(%d)", int32(result))
	}
}

// Action represents an action to be authorized.
type Action struct {
	op       OpType
	arg1     string
	arg2     string
	database string
	trigger  string
}

// Mapping of argument position to concept at:
// https://sqlite.org/c3ref/c_alter_table.html

// Type returns the type of action being authorized.
func (action Action) Type() OpType {
	return action.op
}

// Accessor returns the name of the inner-most trigger or view that is
// responsible for the access attempt or the empty string if this access attempt
// is directly from top-level SQL code.
func (action Action) Accessor() string {
	return action.trigger
}

// Database returns the name of the database (e.g. "main", "temp", etc.) this
// action affects or the empty string if not applicable.
func (action Action) Database() string {
	switch action.op {
	case OpDetach, OpAlterTable:
		return action.arg1
	default:
		return action.database
	}
}

// Index returns the name of the index this action affects or the empty string
// if not applicable.
func (action Action) Index() string {
	switch action.op {
	case OpCreateIndex, OpCreateTempIndex, OpDropIndex, OpDropTempIndex, OpReindex:
		return action.arg1
	default:
		return ""
	}
}

// Table returns the name of the table this action affects or the empty string
// if not applicable.
func (action Action) Table() string {
	switch action.op {
	case OpCreateTable, OpCreateTempTable, OpDelete, OpDropTable, OpDropTempTable, OpInsert, OpRead, OpUpdate, OpAnalyze, OpCreateVTable, OpDropVTable:
		return action.arg1
	case OpCreateIndex, OpCreateTempIndex, OpCreateTempTrigger, OpCreateTrigger, OpDropIndex, OpDropTempIndex, OpDropTempTrigger, OpDropTrigger, OpAlterTable:
		return action.arg2
	default:
		return ""
	}
}

// Trigger returns the name of the trigger this action affects or the empty
// string if not applicable.
func (action Action) Trigger() string {
	switch action.op {
	case OpCreateTempTrigger, OpCreateTrigger, OpDropTempTrigger, OpDropTrigger:
		return action.arg1
	default:
		return ""
	}
}

// View returns the name of the view this action affects or the empty string
// if not applicable.
func (action Action) View() string {
	switch action.op {
	case OpCreateTempView, OpCreateView, OpDropTempView, OpDropView:
		return action.arg1
	default:
		return ""
	}
}

// Pragma returns the name of the action's PRAGMA command or the empty string
// if the action does not represent a PRAGMA command.
// See https://sqlite.org/pragma.html#toc for a list of possible values.
func (action Action) Pragma() string {
	if action.op != OpPragma {
		return ""
	}
	return action.arg1
}

// PragmaArg returns the argument to the PRAGMA command or the empty string if
// the action does not represent a PRAGMA command or the PRAGMA command does not
// take an argument.
func (action Action) PragmaArg() string {
	if action.op != OpPragma {
		return ""
	}
	return action.arg2
}

// Column returns the name of the column this action affects or the empty string
// if not applicable. For OpRead actions, this will return the empty string if a
// table is referenced but no column values are extracted from that table
// (e.g. a query like "SELECT COUNT(*) FROM tab").
func (action Action) Column() string {
	switch action.op {
	case OpRead, OpUpdate:
		return action.arg2
	default:
		return ""
	}
}

// Operation returns one of "BEGIN", "COMMIT", "RELEASE", or "ROLLBACK" for a
// transaction or savepoint statement or the empty string otherwise.
func (action Action) Operation() string {
	switch action.op {
	case OpTransaction, OpSavepoint:
		return action.arg1
	default:
		return ""
	}
}

// File returns the name of the file being ATTACHed or the empty string if the
// action does not represent an ATTACH DATABASE statement.
func (action Action) File() string {
	if action.op != OpAttach {
		return ""
	}
	return action.arg1
}

// Module returns the module name given to the virtual table statement or the
// empty string if the action does not represent a CREATE VIRTUAL TABLE or
// DROP VIRTUAL TABLE statement.
func (action Action) Module() string {
	switch action.op {
	case OpCreateVTable, OpDropVTable:
		return action.arg2
	default:
		return ""
	}
}

// Savepoint returns the name given to the SAVEPOINT statement or the empty
// string if the action does not represent a SAVEPOINT statement.
func (action Action) Savepoint() string {
	if action.op != OpSavepoint {
		return ""
	}
	return action.arg2
}

// String returns a debugging representation of the action.
func (action Action) String() string {
	sb := new(strings.Builder)
	sb.WriteString(action.op.String())
	params := []struct {
		name, value string
	}{
		{"database", action.Database()},
		{"file", action.File()},
		{"trigger", action.Trigger()},
		{"index", action.Index()},
		{"table", action.Table()},
		{"view", action.View()},
		{"module", action.Module()},
		{"column", action.Column()},

		{"operation", action.Operation()},
		{"savepoint", action.Savepoint()},

		{"pragma", action.Pragma()},
		{"arg", action.PragmaArg()},
	}
	for _, p := range params {
		if p.value != "" {
			sb.WriteString(" ")
			sb.WriteString(p.name)
			sb.WriteString(":")
			sb.WriteString(p.value)
		}
	}
	return sb.String()
}
