// Copyright (c) 2018 David Crawshaw <david@zentus.com>
// Copyright (c) 2021 Roxy Light <roxy@zombiezen.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// SPDX-License-Identifier: ISC

package sqlite

import (
	"bytes"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	lib "modernc.org/sqlite/lib"
)

// Version is the SQLite version in the format "X.Y.Z" where X is the major
// version number (always 3), Y is the minor version number, and Z is the
// release number.
const Version = lib.SQLITE_VERSION

// VersionNumber is an integer with the value (X*1000000 + Y*1000 + Z) where X
// is the major version number (always 3), Y is the minor version number, and Z
// is the release number.
const VersionNumber = lib.SQLITE_VERSION_NUMBER

var initOnce sync.Once

func initlib(tls *libc.TLS) {
	initOnce.Do(func() {
		lib.Xsqlite3_initialize(tls)
	})
}

// Conn is an open connection to an SQLite3 database.
//
// A Conn can only be used by one goroutine at a time.
type Conn struct {
	tls    *libc.TLS
	conn   uintptr
	stmts  map[string]*Stmt // query -> prepared statement
	closed bool

	cancelCh   chan struct{}
	doneCh     <-chan struct{}
	unlockNote uintptr
	file       string
	line       int
}

const ptrSize = types.Size_t(unsafe.Sizeof(uintptr(0)))

// OpenConn opens a single SQLite database connection with the given flags.
// No flags or a value of 0 defaults to the following:
//
//   - [OpenReadWrite]
//   - [OpenCreate]
//   - [OpenWAL]
//   - [OpenURI]
//
// The OpenWAL flag is an extension specific to this library:
// it runs "PRAGMA journal_mode=wal;" before returning.
// While convenient, this constitutes a write transaction
// and may fail if the database is contended.
// A default timeout of a few seconds is used
// so that OpenConn does not block indefinitely.
// If you want to have more control over the timeout on setting WAL,
// do not pass OpenWAL and execute "PRAGMA journal_mode=wal;" yourself.
//
// https://www.sqlite.org/c3ref/open.html
func OpenConn(path string, flags ...OpenFlags) (*Conn, error) {
	var openFlags OpenFlags
	for _, f := range flags {
		openFlags |= f
	}
	if openFlags == 0 {
		openFlags = OpenReadWrite | OpenCreate | OpenWAL | OpenURI
	}

	c, err := openConn(path, openFlags&^(OpenWAL|OpenFullMutex)|OpenNoMutex)
	if err != nil {
		return nil, err
	}

	// Disable double-quoted string literals.
	varArgs := libc.NewVaList(int32(0), uintptr(0))
	if varArgs == 0 {
		c.Close()
		return nil, fmt.Errorf("sqlite: open %q: cannot allocate memory", path)
	}
	defer libc.Xfree(c.tls, varArgs)
	res := ResultCode(lib.Xsqlite3_db_config(
		c.tls,
		c.conn,
		lib.SQLITE_DBCONFIG_DQS_DDL,
		varArgs,
	))
	if err := res.ToError(); err != nil {
		// Making error opaque because it's not part of the primary connection
		// opening and reflects an internal error.
		c.Close()
		return nil, fmt.Errorf("sqlite: open %q: disable double-quoted string literals: %v", path, err)
	}
	res = ResultCode(lib.Xsqlite3_db_config(
		c.tls,
		c.conn,
		lib.SQLITE_DBCONFIG_DQS_DML,
		varArgs,
	))
	if err := res.ToError(); err != nil {
		// Making error opaque because it's not part of the primary connection
		// opening and reflects an internal error.
		c.Close()
		return nil, fmt.Errorf("sqlite: open %q: disable double-quoted string literals: %v", path, err)
	}

	if openFlags&OpenWAL != 0 {
		// Set timeout for enabling WAL.
		// See https://github.com/crawshaw/sqlite/pull/113 for details.
		c.SetBusyTimeout(10 * time.Second)

		stmt, _, err := c.PrepareTransient("PRAGMA journal_mode=wal;")
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
		}
		_, err = stmt.Step()
		stmt.Finalize()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("sqlite: open %q: enable wal: %w", path, err)
		}
	}

	c.SetBlockOnBusy()
	return c, nil
}

var allConns struct {
	mu    sync.RWMutex
	table map[uintptr]*Conn
}

func openConn(path string, openFlags OpenFlags) (_ *Conn, err error) {
	tls := libc.NewTLS()
	defer func() {
		if err != nil {
			tls.Close()
		}
	}()
	initlib(tls)
	unlockNote, err := allocUnlockNote(tls)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	defer func() {
		if err != nil {
			libc.Xfree(tls, unlockNote)
		}
	}()
	cpath, err := libc.CString(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	defer libc.Xfree(tls, cpath)
	connPtr, err := malloc(tls, ptrSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	defer libc.Xfree(tls, connPtr)
	res := ResultCode(lib.Xsqlite3_open_v2(tls, cpath, connPtr, int32(openFlags), 0))
	c := &Conn{
		tls:        tls,
		conn:       *(*uintptr)(unsafe.Pointer(connPtr)),
		stmts:      make(map[string]*Stmt),
		unlockNote: unlockNote,
	}
	if c.conn == 0 {
		// Not enough memory to allocate the sqlite3 object.
		return nil, fmt.Errorf("sqlite: open %q: %w", path, res.ToError())
	}
	if res != ResultOK {
		// sqlite3_open_v2 may still return a sqlite3* just so we can extract the error.
		extres := ResultCode(lib.Xsqlite3_extended_errcode(tls, c.conn))
		if extres != 0 {
			res = extres
		}
		lib.Xsqlite3_close_v2(tls, c.conn)
		return nil, fmt.Errorf("sqlite: open %q: %w", path, res.ToError())
	}
	lib.Xsqlite3_extended_result_codes(tls, c.conn, 1)

	allConns.mu.Lock()
	if allConns.table == nil {
		allConns.table = make(map[uintptr]*Conn)
	}
	allConns.table[c.conn] = c
	allConns.mu.Unlock()

	return c, nil
}

// Close closes the database connection using sqlite3_close and finalizes
// persistent prepared statements. https://www.sqlite.org/c3ref/close.html
func (c *Conn) Close() error {
	if c == nil {
		return fmt.Errorf("sqlite: close: nil connection")
	}
	if c.closed {
		return fmt.Errorf("sqlite: close: already closed")
	}
	c.cancelInterrupt()
	c.closed = true
	for _, stmt := range c.stmts {
		stmt.Finalize()
	}
	res := ResultCode(lib.Xsqlite3_close(c.tls, c.conn))
	libc.Xfree(c.tls, c.unlockNote)
	c.unlockNote = 0
	c.tls.Close()
	c.tls = nil
	c.releaseAuthorizer()
	busyHandlers.Delete(c.conn)
	allConns.mu.Lock()
	delete(allConns.table, c.conn)
	allConns.mu.Unlock()
	if !res.IsSuccess() {
		return fmt.Errorf("sqlite: close: %w", res.ToError())
	}
	return nil
}

// AutocommitEnabled reports whether conn is in autocommit mode.
// https://sqlite.org/c3ref/get_autocommit.html
func (c *Conn) AutocommitEnabled() bool {
	if c == nil {
		return false
	}
	return lib.Xsqlite3_get_autocommit(c.tls, c.conn) != 0
}

// CheckReset reports whether any statement on this connection is in the process
// of returning results.
func (c *Conn) CheckReset() string {
	if c != nil {
		for _, stmt := range c.stmts {
			if stmt.lastHasRow {
				return stmt.query
			}
		}
	}
	return ""
}

// SetInterrupt assigns a channel to control connection execution lifetime.
//
// When doneCh is closed, the connection uses sqlite3_interrupt to
// stop long-running queries and cancels any *Stmt.Step calls that
// are blocked waiting for the database write lock.
//
// Subsequent uses of the connection will return SQLITE_INTERRUPT
// errors until doneCh is reset with a subsequent call to SetInterrupt.
//
// Any busy statements at the time SetInterrupt is called will be reset.
//
// SetInterrupt returns the old doneCh assigned to the connection.
func (c *Conn) SetInterrupt(doneCh <-chan struct{}) (oldDoneCh <-chan struct{}) {
	if c == nil {
		return nil
	}
	if c.closed {
		panic("sqlite.Conn is closed")
	}
	oldDoneCh = c.doneCh
	c.cancelInterrupt()
	c.doneCh = doneCh
	for _, stmt := range c.stmts {
		if stmt.lastHasRow {
			stmt.Reset()
		}
	}
	if doneCh == nil {
		return oldDoneCh
	}
	cancelCh := make(chan struct{})
	c.cancelCh = cancelCh
	go func() {
		select {
		case <-doneCh:
			lib.Xsqlite3_interrupt(c.tls, c.conn)
			fireUnlockNote(c.tls, c.unlockNote)
			<-cancelCh
			cancelCh <- struct{}{}
		case <-cancelCh:
			cancelCh <- struct{}{}
		}
	}()
	return oldDoneCh
}

// SetBusyTimeout sets a busy handler that sleeps for up to d to acquire a lock.
// Passing a non-positive value will turn off all busy handlers.
//
// By default, connections are opened with SetBlockOnBusy,
// with the assumption that programs use SetInterrupt to control timeouts.
//
// https://www.sqlite.org/c3ref/busy_timeout.html
func (c *Conn) SetBusyTimeout(d time.Duration) {
	if c != nil {
		lib.Xsqlite3_busy_timeout(c.tls, c.conn, int32(d/time.Millisecond))
		busyHandlers.Delete(c.conn)
	}
}

// SetBlockOnBusy sets a busy handler that waits to acquire a lock
// until the connection is interrupted (see SetInterrupt).
//
// By default, connections are opened with SetBlockOnBusy,
// with the assumption that programs use SetInterrupt to control timeouts.
//
// https://www.sqlite.org/c3ref/busy_handler.html
func (c *Conn) SetBlockOnBusy() {
	if c == nil {
		return
	}
	c.setBusyHandler(func(count int) bool {
		if count >= len(busyDelays) {
			count = len(busyDelays) - 1
		}
		t := time.NewTimer(busyDelays[count])
		defer t.Stop()
		select {
		case <-t.C:
			return true
		case <-c.doneCh:
			// ^ Assuming that doneCh won't be set by SetInterrupt concurrently
			// with other operations.
			return false
		}
	})
}

var busyDelays = [...]time.Duration{
	1 * time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	15 * time.Millisecond,
	20 * time.Millisecond,
	25 * time.Millisecond,
	25 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
}

var busyHandlers sync.Map // sqlite3* -> func(int) bool

func (c *Conn) setBusyHandler(handler func(count int) bool) {
	if c == nil {
		return
	}
	if handler == nil {
		lib.Xsqlite3_busy_handler(c.tls, c.conn, 0, 0)
		busyHandlers.Delete(c.conn)
		return
	}
	busyHandlers.Store(c.conn, handler)
	xBusy := cFuncPointer(busyHandlerCallback)
	lib.Xsqlite3_busy_handler(c.tls, c.conn, xBusy, c.conn)
}

func busyHandlerCallback(tls *libc.TLS, pArg uintptr, count int32) int32 {
	val, _ := busyHandlers.Load(pArg)
	if val == nil {
		return 0
	}
	f := val.(func(int) bool)
	if !f(int(count)) {
		return 0
	}
	return 1
}

func (c *Conn) interrupted() error {
	select {
	case <-c.doneCh:
		return ResultInterrupt.ToError()
	default:
		return nil
	}
}

func (c *Conn) cancelInterrupt() {
	if c.cancelCh != nil {
		c.cancelCh <- struct{}{}
		<-c.cancelCh
		c.cancelCh = nil
	}
}

// Prep returns a persistent SQL statement.
//
// Any error in preparation will panic.
//
// Persistent prepared statements are cached by the query
// string in a Conn. If Finalize is not called, then subsequent
// calls to Prepare will return the same statement.
//
// https://www.sqlite.org/c3ref/prepare.html
func (c *Conn) Prep(query string) *Stmt {
	stmt, err := c.Prepare(query)
	if err != nil {
		if ErrCode(err) == ResultInterrupt {
			return &Stmt{
				conn:          c,
				query:         query,
				colNames:      make(map[string]int),
				prepInterrupt: true,
			}
		}
		panic(err)
	}
	return stmt
}

// Prepare prepares a persistent SQL statement.
//
// Persistent prepared statements are cached by the query
// string in a Conn. If Finalize is not called, then subsequent
// calls to Prepare will return the same statement.
//
// If the query has any unprocessed trailing bytes, Prepare
// returns an error.
//
// https://www.sqlite.org/c3ref/prepare.html
func (c *Conn) Prepare(query string) (*Stmt, error) {
	if c == nil {
		return nil, fmt.Errorf("sqlite: prepare %q: nil connection", query)
	}
	if stmt := c.stmts[query]; stmt != nil {
		if err := stmt.Reset(); err != nil {
			return nil, err
		}
		if err := stmt.ClearBindings(); err != nil {
			return nil, err
		}
		return stmt, nil
	}
	stmt, trailingBytes, err := c.prepare(query, lib.SQLITE_PREPARE_PERSISTENT)
	if err != nil {
		return nil, fmt.Errorf("sqlite: prepare %q: %w", query, err)
	}
	if trailingBytes != 0 {
		stmt.Finalize()
		return nil, fmt.Errorf("sqlite: prepare %q: statement has trailing bytes", query)
	}
	c.stmts[query] = stmt
	return stmt, nil
}

// PrepareTransient prepares an SQL statement that is not cached by
// the Conn. Subsequent calls with the same query will create new Stmts.
// Finalize must be called by the caller once done with the Stmt.
//
// The number of trailing bytes not consumed from query is returned.
//
// To run a sequence of queries once as part of a script,
// the sqlitex package provides an ExecScript function built on this.
//
// https://www.sqlite.org/c3ref/prepare.html
func (c *Conn) PrepareTransient(query string) (stmt *Stmt, trailingBytes int, err error) {
	if c == nil {
		return nil, 0, fmt.Errorf("sqlite: prepare: nil connection")
	}
	// TODO(soon)
	// if stmt != nil {
	// 	runtime.SetFinalizer(stmt, func(stmt *Stmt) {
	// 		if stmt.conn != nil {
	// 			panic("open *sqlite.Stmt \"" + query + "\" garbage collected, call Finalize")
	// 		}
	// 	})
	// }
	stmt, trailingBytes, err = c.prepare(query, 0)
	if err != nil {
		err = fmt.Errorf("sqlite: prepare: %w", err)
	}
	return stmt, trailingBytes, err
}

func (c *Conn) prepare(query string, flags uint32) (*Stmt, int, error) {
	if err := c.interrupted(); err != nil {
		return nil, 0, err
	}

	cquery, err := libc.CString(query)
	if err != nil {
		return nil, 0, err
	}
	defer libc.Xfree(c.tls, cquery)
	ctrailingPtr, err := malloc(c.tls, ptrSize)
	if err != nil {
		return nil, 0, err
	}
	defer libc.Xfree(c.tls, ctrailingPtr)
	stmtPtr, err := malloc(c.tls, ptrSize)
	if err != nil {
		return nil, 0, err
	}
	defer libc.Xfree(c.tls, stmtPtr)
	res := ResultCode(lib.Xsqlite3_prepare_v3(c.tls, c.conn, cquery, -1, flags, stmtPtr, ctrailingPtr))
	if err := c.extreserr(res); err != nil {
		if offset, ok := ErrorOffset(err); ok && offset <= len(query) {
			line, col := linecol(query[:offset])
			err = fmt.Errorf("%d:%d: %w", line, col, err)
		}
		return nil, 0, err
	}
	ctrailing := *(*uintptr)(unsafe.Pointer(ctrailingPtr))
	trailingBytes := len(query) - int(ctrailing-cquery)

	stmt := &Stmt{
		conn:  c,
		query: query,
		stmt:  *(*uintptr)(unsafe.Pointer(stmtPtr)),
	}
	stmt.bindNames = make([]string, lib.Xsqlite3_bind_parameter_count(c.tls, stmt.stmt))
	for i := range stmt.bindNames {
		cname := lib.Xsqlite3_bind_parameter_name(c.tls, stmt.stmt, int32(i+1))
		if cname != 0 {
			stmt.bindNames[i] = libc.GoString(cname)
		}
	}

	colCount := int(lib.Xsqlite3_column_count(c.tls, stmt.stmt))
	stmt.colNames = make(map[string]int, colCount)
	for i := 0; i < colCount; i++ {
		cname := lib.Xsqlite3_column_name(c.tls, stmt.stmt, int32(i))
		if cname != 0 {
			stmt.colNames[libc.GoString(cname)] = i
		}
	}

	return stmt, trailingBytes, nil
}

// Changes reports the number of rows affected by the most recent statement.
//
// https://www.sqlite.org/c3ref/changes.html
func (c *Conn) Changes() int {
	if c == nil {
		return 0
	}
	return int(lib.Xsqlite3_changes(c.tls, c.conn))
}

// LastInsertRowID reports the rowid of the most recently successful INSERT.
//
// https://www.sqlite.org/c3ref/last_insert_rowid.html
func (c *Conn) LastInsertRowID() int64 {
	if c == nil {
		return 0
	}
	return lib.Xsqlite3_last_insert_rowid(c.tls, c.conn)
}

// Serialize serializes the database with the given name (e.g. "main" or "temp").
func (c *Conn) Serialize(dbName string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("sqlite: serialize %q: nil connection", dbName)
	}
	zSchema, cleanup, err := cDBName(dbName)
	if err != nil {
		return nil, fmt.Errorf("sqlite: serialize %q: %v", dbName, err)
	}
	defer cleanup()
	piSize := lib.Xsqlite3_malloc(c.tls, int32(unsafe.Sizeof(int64(0))))
	if piSize == 0 {
		return nil, fmt.Errorf("sqlite: serialize %q: memory allocation failure", dbName)
	}
	defer lib.Xsqlite3_free(c.tls, piSize)

	// Optimization: avoid copying if possible.
	p := lib.Xsqlite3_serialize(c.tls, c.conn, zSchema, piSize, lib.SQLITE_SERIALIZE_NOCOPY)
	if p == 0 {
		// Optimization impossible. Have SQLite allocate memory.
		p = lib.Xsqlite3_serialize(c.tls, c.conn, zSchema, piSize, 0)
		if p == 0 {
			return nil, fmt.Errorf("sqlite: serialize %q: unable to serialize", dbName)
		}
		defer lib.Xsqlite3_free(c.tls, p)
	}

	// Copy data into a Go byte slice.
	n := *(*int64)(unsafe.Pointer(piSize))
	goCopy := make([]byte, n)
	copy(goCopy, libc.GoBytes(p, int(n)))
	return goCopy, nil
}

// Deserialize disconnects the database with the given name (e.g. "main")
// and reopens it as an in-memory database based on the serialized data.
// The database name must already exist.
// It is not possible to deserialize into the TEMP database.
func (c *Conn) Deserialize(dbName string, data []byte) error {
	if c == nil {
		return fmt.Errorf("sqlite: deserialize to %q: nil connection", dbName)
	}
	zSchema, cleanup, err := cDBName(dbName)
	if err != nil {
		return fmt.Errorf("sqlite: deserialize to %q: %v", dbName, err)
	}
	defer cleanup()

	n := int64(len(data))
	pData := lib.Xsqlite3_malloc64(c.tls, uint64(n))
	if pData == 0 {
		return fmt.Errorf("sqlite: deserialize to %q: memory allocation failure", dbName)
	}
	copy(libc.GoBytes(pData, len(data)), data)
	res := ResultCode(lib.Xsqlite3_deserialize(c.tls, c.conn, zSchema, pData, n, n, lib.SQLITE_DESERIALIZE_FREEONCLOSE|lib.SQLITE_DESERIALIZE_RESIZEABLE))
	if !res.IsSuccess() {
		return fmt.Errorf("sqlite: deserialize to %q: %w", dbName, res.ToError())
	}
	return nil
}

// extreserr asks SQLite for a string explaining the error.
// Only called for errors that are probably program bugs.
func (c *Conn) extreserr(res ResultCode) error {
	if res.IsSuccess() {
		return nil
	}
	return &extendedError{
		code:    res,
		offset:  int(lib.Xsqlite3_error_offset(c.tls, c.conn)),
		message: libc.GoString(lib.Xsqlite3_errmsg(c.tls, c.conn)),
	}
}

// linecol computes the 1-based line number and column
// of a character placed after s.
func linecol(s string) (line, col int) {
	line, col = 1, 1
	for _, c := range s {
		// This is intentionally a bit naive with regards to control characters.
		// If the user wants greater control,
		switch c {
		case '\n':
			line++
			col = 1
		case '\t':
			const tabWidth = 8
			locationInTab := (col - 1) % tabWidth
			col += tabWidth - locationInTab
		default:
			col++
		}
	}
	return
}

// Stmt is an SQLite3 prepared statement.
//
// A Stmt is attached to a particular Conn
// (and that Conn can only be used by a single goroutine).
//
// When a Stmt is no longer needed it should be cleaned up
// by calling the Finalize method.
type Stmt struct {
	conn          *Conn
	stmt          uintptr
	query         string
	bindNames     []string
	colNames      map[string]int
	bindErr       error
	prepInterrupt bool // set if Prep was interrupted
	lastHasRow    bool // last bool returned by Step
}

func (stmt *Stmt) interrupted() error {
	if stmt.prepInterrupt {
		return ResultInterrupt.ToError()
	}
	return stmt.conn.interrupted()
}

// Finalize deletes a prepared statement.
//
// Be sure to always call Finalize when done with
// a statement created using PrepareTransient.
//
// Do not call Finalize on a prepared statement that
// you intend to prepare again in the future.
//
// https://www.sqlite.org/c3ref/finalize.html
func (stmt *Stmt) Finalize() error {
	if ptr := stmt.conn.stmts[stmt.query]; ptr == stmt {
		delete(stmt.conn.stmts, stmt.query)
	}
	res := ResultCode(lib.Xsqlite3_finalize(stmt.conn.tls, stmt.stmt))
	stmt.conn = nil
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: finalize: %w", err)
	}
	return nil
}

// Reset resets a prepared statement so it can be executed again.
//
// Note that any parameter values bound to the statement are retained.
// To clear bound values, call ClearBindings.
//
// https://www.sqlite.org/c3ref/reset.html
func (stmt *Stmt) Reset() error {
	stmt.lastHasRow = false
	var res ResultCode
	for {
		res = ResultCode(lib.Xsqlite3_reset(stmt.conn.tls, stmt.stmt))
		if res != ResultLockedSharedCache {
			break
		}
		// An SQLITE_LOCKED_SHAREDCACHE error has been seen from sqlite3_reset
		// in the wild, but so far has eluded exact test case replication.
		// TODO: write a test for this.
		if res := waitForUnlockNotify(stmt.conn.tls, stmt.conn.conn, stmt.conn.unlockNote); res != ResultOK {
			return fmt.Errorf("sqlite: reset: %w", stmt.conn.extreserr(res))
		}
	}
	if err := stmt.conn.extreserr(res); err != nil {
		return fmt.Errorf("sqlite: reset: %w", err)
	}
	return nil
}

// ClearBindings clears all bound parameter values on a statement.
//
// https://www.sqlite.org/c3ref/clear_bindings.html
func (stmt *Stmt) ClearBindings() error {
	if err := stmt.interrupted(); err != nil {
		return fmt.Errorf("sqlite: clear bindings: %w", err)
	}
	res := ResultCode(lib.Xsqlite3_clear_bindings(stmt.conn.tls, stmt.stmt))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: clear bindings: %w", err)
	}
	return nil
}

// Step moves through the statement cursor using sqlite3_step.
//
// If a row of data is available, rowReturned is reported as true.
// If the statement has reached the end of the available data then
// rowReturned is false. Thus the status codes SQLITE_ROW and
// SQLITE_DONE are reported by the rowReturned bool, and all other
// non-OK status codes are reported as an error.
//
// If an error value is returned, then the statement has been reset.
//
// https://www.sqlite.org/c3ref/step.html
//
// # Shared cache
//
// As multiple writers are common in multi-threaded programs,
// this Step method uses sqlite3_unlock_notify to handle any
// SQLITE_LOCKED errors.
//
// Without the shared cache, SQLite will block for
// several seconds while trying to acquire the write lock.
// With the shared cache, it returns SQLITE_LOCKED immediately
// if the write lock is held by another connection in this process.
// Dealing with this correctly makes for an unpleasant programming
// experience, so this package does it automatically by blocking
// Step until the write lock is relinquished.
//
// This means Step can block for a very long time.
// Use SetInterrupt to control how long Step will block.
//
// For far more details, see:
//
//	http://www.sqlite.org/unlock_notify.html
func (stmt *Stmt) Step() (rowReturned bool, err error) {
	if stmt.bindErr != nil {
		err = stmt.bindErr
		stmt.bindErr = nil
		stmt.Reset()
		return false, fmt.Errorf("sqlite: step: %w", err)
	}
	rowReturned, err = stmt.step()
	stmt.lastHasRow = rowReturned
	if err != nil {
		lib.Xsqlite3_reset(stmt.conn.tls, stmt.stmt)
		return rowReturned, fmt.Errorf("sqlite: step: %w", err)
	}
	return rowReturned, nil
}

func (stmt *Stmt) step() (bool, error) {
	for {
		if err := stmt.interrupted(); err != nil {
			return false, err
		}
		switch res := ResultCode(lib.Xsqlite3_step(stmt.conn.tls, stmt.stmt)); res.ToPrimary() {
		case ResultLocked:
			if res != ResultLockedSharedCache {
				// don't call waitForUnlockNotify as it might deadlock, see:
				// https://github.com/crawshaw/sqlite/issues/6
				return false, stmt.conn.extreserr(res)
			}

			if res := waitForUnlockNotify(stmt.conn.tls, stmt.conn.conn, stmt.conn.unlockNote); !res.IsSuccess() {
				return false, stmt.conn.extreserr(res)
			}
			lib.Xsqlite3_reset(stmt.conn.tls, stmt.stmt)
			// loop
		case ResultRow:
			return true, nil
		case ResultDone:
			return false, nil
		case ResultInterrupt:
			// TODO: embed some of these errors into the stmt for zero-alloc errors?
			return false, res.ToError()
		default:
			return false, stmt.conn.extreserr(res)
		}
	}
}

func (stmt *Stmt) handleBindErr(prefix string, res ResultCode) {
	if stmt.bindErr == nil && !res.IsSuccess() {
		stmt.bindErr = fmt.Errorf("%s: %w", prefix, res.ToError())
	}
}

// DataCount returns the number of columns in the current row of the result
// set of prepared statement.
//
// https://sqlite.org/c3ref/data_count.html
func (stmt *Stmt) DataCount() int {
	return int(lib.Xsqlite3_data_count(stmt.conn.tls, stmt.stmt))
}

// ColumnCount returns the number of columns in the result set returned by the
// prepared statement.
//
// https://sqlite.org/c3ref/column_count.html
func (stmt *Stmt) ColumnCount() int {
	return int(lib.Xsqlite3_column_count(stmt.conn.tls, stmt.stmt))
}

// ColumnName returns the name assigned to a particular column in the result
// set of a SELECT statement.
//
// https://sqlite.org/c3ref/column_name.html
func (stmt *Stmt) ColumnName(col int) string {
	return libc.GoString(lib.Xsqlite3_column_name(stmt.conn.tls, stmt.stmt, int32(col)))
}

// BindParamCount reports the number of parameters in stmt.
//
// https://www.sqlite.org/c3ref/bind_parameter_count.html
func (stmt *Stmt) BindParamCount() int {
	if stmt.stmt == 0 {
		return 0
	}
	return len(stmt.bindNames)
}

// BindParamName returns the name of parameter or the empty string if the
// parameter is nameless or i is out of range.
//
// Parameters indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_parameter_name.html
func (stmt *Stmt) BindParamName(i int) string {
	i-- // map from 1-based to 0-based
	if i < 0 || i >= len(stmt.bindNames) {
		return ""
	}
	return stmt.bindNames[i]
}

// BindInt64 binds value to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindInt64(param int, value int64) {
	if stmt.stmt == 0 {
		return
	}
	res := ResultCode(lib.Xsqlite3_bind_int64(stmt.conn.tls, stmt.stmt, int32(param), value))
	stmt.handleBindErr("bind int64", res)
}

// BindBool binds value (as an integer 0 or 1) to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindBool(param int, value bool) {
	if stmt.stmt == 0 {
		return
	}
	v := int64(0)
	if value {
		v = 1
	}
	res := ResultCode(lib.Xsqlite3_bind_int64(stmt.conn.tls, stmt.stmt, int32(param), v))
	stmt.handleBindErr("bind bool", res)
}

var emptyCString = mustCString("")

const sqliteStatic uintptr = 0

// BindBytes binds value to a numbered stmt parameter.
//
// In-memory copies of value are made using this interface.
// For large blobs, consider using the streaming Blob object.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindBytes(param int, value []byte) {
	if stmt.stmt == 0 {
		return
	}
	if len(value) == 0 {
		res := ResultCode(lib.Xsqlite3_bind_blob(stmt.conn.tls, stmt.stmt, int32(param), emptyCString, 0, sqliteStatic))
		stmt.handleBindErr("bind bytes", res)
		return
	}
	v, err := malloc(stmt.conn.tls, types.Size_t(len(value)))
	if err != nil {
		if stmt.bindErr == nil {
			stmt.bindErr = fmt.Errorf("bind bytes: %w", err)
		}
		return
	}
	for i, b := range value {
		*(*byte)(unsafe.Pointer(v + uintptr(i))) = b
	}
	res := ResultCode(lib.Xsqlite3_bind_blob(stmt.conn.tls, stmt.stmt, int32(param), v, int32(len(value)), freeFuncPtr))
	stmt.handleBindErr("bind bytes", res)
}

var freeFuncPtr = cFuncPointer(libc.Xfree)

// BindText binds value to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindText(param int, value string) {
	if stmt.stmt == 0 {
		return
	}
	allocSize := types.Size_t(len(value))
	if allocSize == 0 {
		allocSize = 1
	}
	v, err := malloc(stmt.conn.tls, allocSize)
	if err != nil {
		if stmt.bindErr == nil {
			stmt.bindErr = fmt.Errorf("bind text: %w", err)
		}
		return
	}
	for i := 0; i < len(value); i++ {
		*(*byte)(unsafe.Pointer(v + uintptr(i))) = value[i]
	}
	res := ResultCode(lib.Xsqlite3_bind_text(stmt.conn.tls, stmt.stmt, int32(param), v, int32(len(value)), freeFuncPtr))
	stmt.handleBindErr("bind text", res)
}

// BindFloat binds value to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindFloat(param int, value float64) {
	if stmt.stmt == 0 {
		return
	}
	res := ResultCode(lib.Xsqlite3_bind_double(stmt.conn.tls, stmt.stmt, int32(param), value))
	stmt.handleBindErr("bind float", res)
}

// BindNull binds an SQL NULL value to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindNull(param int) {
	if stmt.stmt == 0 {
		return
	}
	res := ResultCode(lib.Xsqlite3_bind_null(stmt.conn.tls, stmt.stmt, int32(param)))
	stmt.handleBindErr("bind null", res)
}

// BindZeroBlob binds a blob of zeros of length len to a numbered stmt parameter.
//
// Parameter indices start at 1.
//
// https://www.sqlite.org/c3ref/bind_blob.html
func (stmt *Stmt) BindZeroBlob(param int, len int64) {
	if stmt.stmt == 0 {
		return
	}
	res := ResultCode(lib.Xsqlite3_bind_zeroblob64(stmt.conn.tls, stmt.stmt, int32(param), uint64(len)))
	stmt.handleBindErr("bind zero blob", res)
}

func (stmt *Stmt) findBindName(prefix string, param string) int {
	for i, name := range stmt.bindNames {
		if name == param {
			return i + 1 // 1-based indices
		}
	}
	if stmt.bindErr == nil {
		stmt.bindErr = fmt.Errorf("%s: unknown parameter: %s", prefix, param)
	}
	return 0
}

// SetInt64 binds an int64 to a parameter using a column name.
func (stmt *Stmt) SetInt64(param string, value int64) {
	stmt.BindInt64(stmt.findBindName("SetInt64", param), value)
}

// SetBool binds a value (as a 0 or 1) to a parameter using a column name.
func (stmt *Stmt) SetBool(param string, value bool) {
	stmt.BindBool(stmt.findBindName("SetBool", param), value)
}

// SetBytes binds bytes to a parameter using a column name.
// An invalid parameter name will cause the call to Step to return an error.
func (stmt *Stmt) SetBytes(param string, value []byte) {
	stmt.BindBytes(stmt.findBindName("SetBytes", param), value)
}

// SetText binds text to a parameter using a column name.
// An invalid parameter name will cause the call to Step to return an error.
func (stmt *Stmt) SetText(param string, value string) {
	stmt.BindText(stmt.findBindName("SetText", param), value)
}

// SetFloat binds a float64 to a parameter using a column name.
// An invalid parameter name will cause the call to Step to return an error.
func (stmt *Stmt) SetFloat(param string, value float64) {
	stmt.BindFloat(stmt.findBindName("SetFloat", param), value)
}

// SetNull binds a null to a parameter using a column name.
// An invalid parameter name will cause the call to Step to return an error.
func (stmt *Stmt) SetNull(param string) {
	stmt.BindNull(stmt.findBindName("SetNull", param))
}

// SetZeroBlob binds a zero blob of length len to a parameter using a column name.
// An invalid parameter name will cause the call to Step to return an error.
func (stmt *Stmt) SetZeroBlob(param string, len int64) {
	stmt.BindZeroBlob(stmt.findBindName("SetZeroBlob", param), len)
}

// ColumnInt returns a query result value as an int.
//
// Note: this method calls sqlite3_column_int64 and then converts the
// resulting 64-bits to an int.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnInt(col int) int {
	return int(stmt.ColumnInt64(col))
}

// ColumnInt32 returns a query result value as an int32.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnInt32(col int) int32 {
	return lib.Xsqlite3_column_int(stmt.conn.tls, stmt.stmt, int32(col))
}

// ColumnInt64 returns a query result value as an int64.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnInt64(col int) int64 {
	return lib.Xsqlite3_column_int64(stmt.conn.tls, stmt.stmt, int32(col))
}

// ColumnBool reports whether a query result value is non-zero.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnBool(col int) bool {
	return stmt.ColumnInt64(col) != 0
}

// ColumnBytes reads a query result into buf.
// It reports the number of bytes read.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnBytes(col int, buf []byte) int {
	return copy(buf, stmt.columnBytes(col))
}

// ColumnReader creates a byte reader for a query result column.
//
// The reader directly references C-managed memory that stops
// being valid as soon as the statement row resets.
func (stmt *Stmt) ColumnReader(col int) *bytes.Reader {
	// Load the C memory directly into the Reader.
	// There is no exported method that lets it escape.
	return bytes.NewReader(stmt.columnBytes(col))
}

func (stmt *Stmt) columnBytes(col int) []byte {
	p := lib.Xsqlite3_column_blob(stmt.conn.tls, stmt.stmt, int32(col))
	if p == 0 {
		return nil
	}
	n := stmt.ColumnLen(col)
	return libc.GoBytes(p, n)
}

// ColumnType are codes for each of the SQLite fundamental datatypes:
//
//   - 64-bit signed integer
//   - 64-bit IEEE floating point number
//   - string
//   - BLOB
//   - NULL
//
// https://www.sqlite.org/c3ref/c_blob.html
type ColumnType int

// Data types.
const (
	TypeInteger ColumnType = lib.SQLITE_INTEGER
	TypeFloat   ColumnType = lib.SQLITE_FLOAT
	TypeText    ColumnType = lib.SQLITE_TEXT
	TypeBlob    ColumnType = lib.SQLITE_BLOB
	TypeNull    ColumnType = lib.SQLITE_NULL
)

// String returns the SQLite constant name of the type.
func (t ColumnType) String() string {
	switch t {
	case TypeInteger:
		return "SQLITE_INTEGER"
	case TypeFloat:
		return "SQLITE_FLOAT"
	case TypeText:
		return "SQLITE_TEXT"
	case TypeBlob:
		return "SQLITE_BLOB"
	case TypeNull:
		return "SQLITE_NULL"
	default:
		return "<unknown sqlite datatype>"
	}
}

// ColumnType returns the datatype code for the initial data
// type of the result column. The returned value is one of:
//
//   - SQLITE_INTEGER
//   - SQLITE_FLOAT
//   - SQLITE_TEXT
//   - SQLITE_BLOB
//   - SQLITE_NULL
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnType(col int) ColumnType {
	return ColumnType(lib.Xsqlite3_column_type(stmt.conn.tls, stmt.stmt, int32(col)))
}

// ColumnIsNull reports whether the result column holds NULL.
// Column indices start at 0.
func (stmt *Stmt) ColumnIsNull(col int) bool {
	return stmt.ColumnType(col) == TypeNull
}

// ColumnText returns a query result as a string.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnText(col int) string {
	n := stmt.ColumnLen(col)
	return goStringN(lib.Xsqlite3_column_text(stmt.conn.tls, stmt.stmt, int32(col)), n)
}

// ColumnFloat returns a query result as a float64.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnFloat(col int) float64 {
	return lib.Xsqlite3_column_double(stmt.conn.tls, stmt.stmt, int32(col))
}

// ColumnLen returns the number of bytes in a query result.
//
// Column indices start at 0.
//
// https://www.sqlite.org/c3ref/column_blob.html
func (stmt *Stmt) ColumnLen(col int) int {
	return int(lib.Xsqlite3_column_bytes(stmt.conn.tls, stmt.stmt, int32(col)))
}

func (stmt *Stmt) ColumnDatabaseName(col int) string {
	return libc.GoString(lib.Xsqlite3_column_database_name(stmt.conn.tls, stmt.stmt, int32(col)))
}

func (stmt *Stmt) ColumnTableName(col int) string {
	return libc.GoString(lib.Xsqlite3_column_table_name(stmt.conn.tls, stmt.stmt, int32(col)))
}

// ColumnIndex returns the index of the column with the given name.
//
// If there is no column with the given name ColumnIndex returns -1.
func (stmt *Stmt) ColumnIndex(colName string) int {
	col, found := stmt.colNames[colName]
	if !found {
		return -1
	}
	return col
}

// GetInt64 returns a query result value for colName as an int64.
func (stmt *Stmt) GetInt64(colName string) int64 {
	col, found := stmt.colNames[colName]
	if !found {
		return 0
	}
	return stmt.ColumnInt64(col)
}

// GetBool reports whether the query result value for colName is non-zero.
func (stmt *Stmt) GetBool(colName string) bool {
	return stmt.GetInt64(colName) != 0
}

// GetBytes reads a query result for colName into buf.
// It reports the number of bytes read.
func (stmt *Stmt) GetBytes(colName string, buf []byte) int {
	col, found := stmt.colNames[colName]
	if !found {
		return 0
	}
	return stmt.ColumnBytes(col, buf)
}

// GetReader creates a byte reader for colName.
//
// The reader directly references C-managed memory that stops
// being valid as soon as the statement row resets.
func (stmt *Stmt) GetReader(colName string) *bytes.Reader {
	col, found := stmt.colNames[colName]
	if !found {
		return bytes.NewReader(nil)
	}
	return stmt.ColumnReader(col)
}

// GetText returns a query result value for colName as a string.
func (stmt *Stmt) GetText(colName string) string {
	col, found := stmt.colNames[colName]
	if !found {
		return ""
	}
	return stmt.ColumnText(col)
}

// GetFloat returns a query result value for colName as a float64.
func (stmt *Stmt) GetFloat(colName string) float64 {
	col, found := stmt.colNames[colName]
	if !found {
		return 0
	}
	return stmt.ColumnFloat(col)
}

// GetLen returns the number of bytes in a query result for colName.
func (stmt *Stmt) GetLen(colName string) int {
	col, found := stmt.colNames[colName]
	if !found {
		return 0
	}
	return stmt.ColumnLen(col)
}

// IsNull reports whether a query result value for colName is NULL.
func (stmt *Stmt) IsNull(colName string) bool {
	col, found := stmt.colNames[colName]
	if !found {
		return true
	}
	return stmt.ColumnIsNull(col)
}

func malloc(tls *libc.TLS, n types.Size_t) (uintptr, error) {
	p := libc.Xmalloc(tls, n)
	if p == 0 {
		return 0, fmt.Errorf("out of memory")
	}
	return p, nil
}

func mustCString(s string) uintptr {
	p, err := libc.CString(s)
	if err != nil {
		panic(err)
	}
	return p
}

func goStringN(s uintptr, n int) string {
	if s == 0 {
		return ""
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(s)), n)
	return string(b)
}

// cFuncPointer converts a function defined by a function declaration to a C pointer.
// The result of using cFuncPointer on closures is undefined.
func cFuncPointer[T any](f T) uintptr {
	// This assumes the memory representation described in https://golang.org/s/go11func.
	//
	// cFuncPointer does its conversion by doing the following in order:
	// 1) Create a Go struct containing a pointer to a pointer to
	//    the function. It is assumed that the pointer to the function will be
	//    stored in the read-only data section and thus will not move.
	// 2) Convert the pointer to the Go struct to a pointer to uintptr through
	//    unsafe.Pointer. This is permitted via Rule #1 of unsafe.Pointer.
	// 3) Dereference the pointer to uintptr to obtain the function value as a
	//    uintptr. This is safe as long as function values are passed as pointers.
	return *(*uintptr)(unsafe.Pointer(&struct{ f T }{f}))
}

// Limit is a category of performance limits.
//
// https://sqlite.org/c3ref/c_limit_attached.html
type Limit int32

// Limit categories.
const (
	LimitLength            Limit = lib.SQLITE_LIMIT_LENGTH
	LimitSQLLength         Limit = lib.SQLITE_LIMIT_SQL_LENGTH
	LimitColumn            Limit = lib.SQLITE_LIMIT_COLUMN
	LimitExprDepth         Limit = lib.SQLITE_LIMIT_EXPR_DEPTH
	LimitCompoundSelect    Limit = lib.SQLITE_LIMIT_COMPOUND_SELECT
	LimitVDBEOp            Limit = lib.SQLITE_LIMIT_VDBE_OP
	LimitFunctionArg       Limit = lib.SQLITE_LIMIT_FUNCTION_ARG
	LimitAttached          Limit = lib.SQLITE_LIMIT_ATTACHED
	LimitLikePatternLength Limit = lib.SQLITE_LIMIT_LIKE_PATTERN_LENGTH
	LimitVariableNumber    Limit = lib.SQLITE_LIMIT_VARIABLE_NUMBER
	LimitTriggerDepth      Limit = lib.SQLITE_LIMIT_TRIGGER_DEPTH
	LimitWorkerThreads     Limit = lib.SQLITE_LIMIT_WORKER_THREADS
)

// String returns the limit's C constant name.
func (limit Limit) String() string {
	switch limit {
	case LimitLength:
		return "SQLITE_LIMIT_LENGTH"
	case LimitSQLLength:
		return "SQLITE_LIMIT_SQL_LENGTH"
	case LimitColumn:
		return "SQLITE_LIMIT_COLUMN"
	case LimitExprDepth:
		return "SQLITE_LIMIT_EXPR_DEPTH"
	case LimitCompoundSelect:
		return "SQLITE_LIMIT_COMPOUND_SELECT"
	case LimitVDBEOp:
		return "SQLITE_LIMIT_VDBE_OP"
	case LimitFunctionArg:
		return "SQLITE_LIMIT_FUNCTION_ARG"
	case LimitAttached:
		return "SQLITE_LIMIT_ATTACHED"
	case LimitLikePatternLength:
		return "SQLITE_LIMIT_LIKE_PATTERN_LENGTH"
	case LimitVariableNumber:
		return "SQLITE_LIMIT_VARIABLE_NUMBER"
	case LimitTriggerDepth:
		return "SQLITE_LIMIT_TRIGGER_DEPTH"
	case LimitWorkerThreads:
		return "SQLITE_LIMIT_WORKER_THREADS"
	default:
		return fmt.Sprintf("Limit(%d)", int32(limit))
	}
}

// Limit sets a runtime limit on the connection. The the previous value of the
// limit is returned. Pass a negative value to check the limit without changing
// it.
//
// https://sqlite.org/c3ref/limit.html
func (c *Conn) Limit(id Limit, value int32) int32 {
	if c == nil {
		return 0
	}
	return lib.Xsqlite3_limit(c.tls, c.conn, int32(id), int32(value))
}

// SetDefensive sets the "defensive" flag for a database connection. When the
// defensive flag is enabled, language features that allow ordinary SQL to
// deliberately corrupt the database file are disabled. The disabled features
// include but are not limited to the following:
//
//   - The PRAGMA writable_schema=ON statement.
//   - The PRAGMA journal_mode=OFF statement.
//   - Writes to the sqlite_dbpage virtual table.
//   - Direct writes to shadow tables.
func (c *Conn) SetDefensive(enabled bool) error {
	if c == nil {
		return fmt.Errorf("sqlite: set defensive=%t: nil connection", enabled)
	}
	enabledInt := int32(0)
	if enabled {
		enabledInt = 1
	}
	varArgs := libc.NewVaList(enabledInt)
	if varArgs == 0 {
		return fmt.Errorf("sqlite: set defensive=%t: cannot allocate memory", enabled)
	}
	defer libc.Xfree(c.tls, varArgs)

	res := ResultCode(lib.Xsqlite3_db_config(
		c.tls,
		c.conn,
		lib.SQLITE_DBCONFIG_DEFENSIVE,
		varArgs,
	))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: set defensive=%t: %w", enabled, err)
	}
	return nil
}
