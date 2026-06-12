// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2012 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type mysqlConn struct {
	buf              buffer
	netConn          net.Conn
	rawConn          net.Conn    // underlying connection when netConn is TLS connection.
	result           mysqlResult // managed by clearResult() and handleOkPacket().
	compIO           *compIO
	cfg              *Config
	connector        *connector
	maxAllowedPacket int
	maxWriteSize     int
	capabilities     capabilityFlag
	extCapabilities  extendedCapabilityFlag
	status           statusFlag
	sequence         uint8
	compressSequence uint8
	parseTime        bool
	compress         bool

	// for context support (Go 1.8+)
	watching bool
	watcher  chan<- context.Context
	closech  chan struct{}
	finished chan<- struct{}
	canceled atomicError // set non-nil if conn is canceled
	closed   atomic.Bool // set when conn is closed, before closech is closed
}

// Helper function to call per-connection logger.
func (mc *mysqlConn) log(v ...any) {
	_, filename, lineno, ok := runtime.Caller(1)
	if ok {
		pos := strings.LastIndexByte(filename, '/')
		if pos != -1 {
			filename = filename[pos+1:]
		}
		prefix := fmt.Sprintf("%s:%d ", filename, lineno)
		v = append([]any{prefix}, v...)
	}

	mc.cfg.Logger.Print(v...)
}

func (mc *mysqlConn) readWithTimeout(b []byte) (int, error) {
	to := mc.cfg.ReadTimeout
	if to > 0 {
		if err := mc.netConn.SetReadDeadline(time.Now().Add(to)); err != nil {
			return 0, err
		}
	}
	return mc.netConn.Read(b)
}

func (mc *mysqlConn) writeWithTimeout(b []byte) (int, error) {
	to := mc.cfg.WriteTimeout
	if to > 0 {
		if err := mc.netConn.SetWriteDeadline(time.Now().Add(to)); err != nil {
			return 0, err
		}
	}
	return mc.netConn.Write(b)
}

func (mc *mysqlConn) resetSequence() {
	mc.sequence = 0
	mc.compressSequence = 0
}

// syncSequence must be called when finished writing some packet and before start reading.
func (mc *mysqlConn) syncSequence() {
	// Syncs compressionSequence to sequence.
	// This is not documented but done in `net_flush()` in MySQL and MariaDB.
	// https://github.com/mariadb-corporation/mariadb-connector-c/blob/8228164f850b12353da24df1b93a1e53cc5e85e9/libmariadb/ma_net.c#L170-L171
	// https://github.com/mysql/mysql-server/blob/824e2b4064053f7daf17d7f3f84b7a3ed92e5fb4/sql-common/net_serv.cc#L293
	if mc.compress {
		mc.sequence = mc.compressSequence
		mc.compIO.reset()
	}
}

// Handles parameters set in DSN after the connection is established
func (mc *mysqlConn) handleParams() (err error) {
	var cmdSet strings.Builder

	for param, val := range mc.cfg.Params {
		if cmdSet.Len() == 0 {
			// Heuristic: 29 chars for each other key=value to reduce reallocations
			cmdSet.Grow(4 + len(param) + 3 + len(val) + 30*(len(mc.cfg.Params)-1))
			cmdSet.WriteString("SET ")
		} else {
			cmdSet.WriteString(", ")
		}
		cmdSet.WriteString(param)
		cmdSet.WriteString(" = ")
		cmdSet.WriteString(val)
	}

	if cmdSet.Len() > 0 {
		err = mc.exec(cmdSet.String())
	}

	return
}

// markBadConn replaces errBadConnNoWrite with driver.ErrBadConn.
// This function is used to return driver.ErrBadConn only when safe to retry.
func (mc *mysqlConn) markBadConn(err error) error {
	if err == errBadConnNoWrite {
		return driver.ErrBadConn
	}
	return err
}

func (mc *mysqlConn) Begin() (driver.Tx, error) {
	return mc.begin(false)
}

func (mc *mysqlConn) begin(readOnly bool) (driver.Tx, error) {
	if mc.closed.Load() {
		return nil, driver.ErrBadConn
	}
	var q string
	if readOnly {
		q = "START TRANSACTION READ ONLY"
	} else {
		q = "START TRANSACTION"
	}
	err := mc.exec(q)
	if err == nil {
		return &mysqlTx{mc}, err
	}
	return nil, mc.markBadConn(err)
}

func (mc *mysqlConn) Close() (err error) {
	// Makes Close idempotent
	if !mc.closed.Load() {
		err = mc.writeCommandPacket(comQuit)
	}
	mc.close()
	return
}

// close closes the network connection and clear results without sending COM_QUIT.
func (mc *mysqlConn) close() {
	mc.cleanup()
	mc.clearResult()
}

// Closes the network connection and unsets internal variables. Do not call this
// function after successful authentication, call Close instead. This function
// is called before auth or on auth failure because MySQL will have already
// closed the network connection.
func (mc *mysqlConn) cleanup() {
	if mc.closed.Swap(true) {
		return
	}

	// Makes cleanup idempotent
	close(mc.closech)
	conn := mc.rawConn
	if conn == nil {
		return
	}
	if err := conn.Close(); err != nil {
		mc.log("closing connection:", err)
	}
	// This function can be called from multiple goroutines.
	// So we can not mc.clearResult() here.
	// Caller should do it if they are in safe goroutine.
}

func (mc *mysqlConn) error() error {
	if mc.closed.Load() {
		if err := mc.canceled.Value(); err != nil {
			return err
		}
		return ErrInvalidConn
	}
	return nil
}

func (mc *mysqlConn) Prepare(query string) (driver.Stmt, error) {
	if mc.closed.Load() {
		return nil, driver.ErrBadConn
	}
	// Send command
	err := mc.writeCommandPacketStr(comStmtPrepare, query)
	if err != nil {
		// STMT_PREPARE is safe to retry.  So we can return ErrBadConn here.
		mc.log(err)
		return nil, driver.ErrBadConn
	}

	stmt := &mysqlStmt{
		mc: mc,
	}

	// Read Result
	columnCount, err := stmt.readPrepareResultPacket()
	if err == nil {
		if stmt.paramCount > 0 {
			if err = mc.skipColumns(stmt.paramCount); err != nil {
				return nil, err
			}
		}

		if columnCount > 0 {
			if mc.extCapabilities&clientCacheMetadata != 0 {
				if stmt.columns, err = mc.readColumns(int(columnCount), nil); err != nil {
					return nil, err
				}
			} else {
				if err = mc.skipColumns(int(columnCount)); err != nil {
					return nil, err
				}
			}
		}
	}

	return stmt, err
}

func (mc *mysqlConn) interpolateParams(query string, args []driver.Value) (string, error) {
	noBackslashEscapes := (mc.status & statusNoBackslashEscapes) != 0
	const (
		stateNormal = iota
		stateString
		stateEscape
		stateEOLComment
		stateSlashStarComment
		stateBacktick
	)

	const (
		QUOTE_BYTE         = byte('\'')
		DBL_QUOTE_BYTE     = byte('"')
		BACKSLASH_BYTE     = byte('\\')
		QUESTION_MARK_BYTE = byte('?')
		SLASH_BYTE         = byte('/')
		STAR_BYTE          = byte('*')
		HASH_BYTE          = byte('#')
		MINUS_BYTE         = byte('-')
		LINE_FEED_BYTE     = byte('\n')
		BACKTICK_BYTE      = byte('`')
	)

	buf, err := mc.buf.takeCompleteBuffer()
	if err != nil {
		mc.cleanup()
		return "", driver.ErrBadConn
	}
	buf = buf[:0]
	state := stateNormal
	singleQuotes := false
	lastChar := byte(0)
	argPos := 0
	lenQuery := len(query)
	lastIdx := 0

	for i := range lenQuery {
		currentChar := query[i]
		if state == stateEscape && !((currentChar == QUOTE_BYTE && singleQuotes) || (currentChar == DBL_QUOTE_BYTE && !singleQuotes)) {
			state = stateString
			lastChar = currentChar
			continue
		}
		switch currentChar {
		case STAR_BYTE:
			if state == stateNormal && lastChar == SLASH_BYTE {
				state = stateSlashStarComment
			}
		case SLASH_BYTE:
			if state == stateSlashStarComment && lastChar == STAR_BYTE {
				state = stateNormal
				// Clear lastChar so the '/' that closed the comment isn't
				// reused to start a new comment with a following '*'.
				lastChar = 0
				continue
			}
		case HASH_BYTE:
			if state == stateNormal {
				state = stateEOLComment
			}
		case MINUS_BYTE:
			if state == stateNormal && lastChar == MINUS_BYTE {
				// -- only starts a comment if followed by whitespace or control char
				if i+1 < lenQuery {
					nextChar := query[i+1]
					if nextChar == ' ' || nextChar == '\t' || nextChar == '\n' || nextChar == '\r' {
						state = stateEOLComment
					}
				} else {
					state = stateEOLComment
				}
			}
		case LINE_FEED_BYTE:
			if state == stateEOLComment {
				state = stateNormal
			}
		case DBL_QUOTE_BYTE:
			if state == stateNormal {
				state = stateString
				singleQuotes = false
			} else if state == stateString && !singleQuotes {
				state = stateNormal
			} else if state == stateEscape {
				state = stateString
			}
		case QUOTE_BYTE:
			if state == stateNormal {
				state = stateString
				singleQuotes = true
			} else if state == stateString && singleQuotes {
				state = stateNormal
			} else if state == stateEscape {
				state = stateString
			}
		case BACKSLASH_BYTE:
			if state == stateString && !noBackslashEscapes {
				state = stateEscape
			}
		case QUESTION_MARK_BYTE:
			if state == stateNormal {
				if argPos >= len(args) {
					return "", driver.ErrSkip
				}
				buf = append(buf, query[lastIdx:i]...)
				arg := args[argPos]
				argPos++

				if arg == nil {
					buf = append(buf, "NULL"...)
					lastIdx = i + 1
					break
				}

				switch v := arg.(type) {
				case int64:
					buf = strconv.AppendInt(buf, v, 10)
				case uint64:
					buf = strconv.AppendUint(buf, v, 10)
				case float64:
					buf = strconv.AppendFloat(buf, v, 'g', -1, 64)
				case bool:
					if v {
						buf = append(buf, '1')
					} else {
						buf = append(buf, '0')
					}
				case time.Time:
					if v.IsZero() {
						buf = append(buf, "'0000-00-00'"...)
					} else {
						buf = append(buf, '\'')
						buf, err = appendDateTime(buf, v.In(mc.cfg.Loc), mc.cfg.timeTruncate)
						if err != nil {
							return "", err
						}
						buf = append(buf, '\'')
					}
				case json.RawMessage:
					if noBackslashEscapes {
						buf = escapeBytesQuotes(buf, v, false)
					} else {
						buf = escapeBytesBackslash(buf, v, false)
					}
				case []byte:
					if v == nil {
						buf = append(buf, "NULL"...)
					} else {
						if noBackslashEscapes {
							buf = escapeBytesQuotes(buf, v, true)
						} else {
							buf = escapeBytesBackslash(buf, v, true)
						}
					}
				case string:
					if noBackslashEscapes {
						buf = escapeStringQuotes(buf, v)
					} else {
						buf = escapeStringBackslash(buf, v)
					}
				default:
					return "", driver.ErrSkip
				}

				if len(buf)+4 > mc.maxAllowedPacket {
					return "", driver.ErrSkip
				}
				lastIdx = i + 1
			}
		case BACKTICK_BYTE:
			if state == stateBacktick {
				state = stateNormal
			} else if state == stateNormal {
				state = stateBacktick
			}
		}
		lastChar = currentChar
	}
	buf = append(buf, query[lastIdx:]...)
	if argPos != len(args) {
		return "", driver.ErrSkip
	}
	return string(buf), nil
}

func (mc *mysqlConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	if mc.closed.Load() {
		return nil, driver.ErrBadConn
	}
	if len(args) != 0 {
		if !mc.cfg.InterpolateParams {
			return nil, driver.ErrSkip
		}
		// try to interpolate the parameters to save extra roundtrips for preparing and closing a statement
		prepared, err := mc.interpolateParams(query, args)
		if err != nil {
			return nil, err
		}
		query = prepared
	}

	err := mc.exec(query)
	if err == nil {
		copied := mc.result
		return &copied, err
	}
	return nil, mc.markBadConn(err)
}

// Internal function to execute commands
func (mc *mysqlConn) exec(query string) error {
	handleOk := mc.clearResult()
	// Send command
	if err := mc.writeCommandPacketStr(comQuery, query); err != nil {
		return mc.markBadConn(err)
	}

	// Read Result
	resLen, _, err := handleOk.readResultSetHeaderPacket()
	if err != nil {
		return err
	}

	if resLen > 0 {
		// columns
		if err := mc.skipColumns(resLen); err != nil {
			return err
		}

		// rows
		if err := mc.skipRows(); err != nil {
			return err
		}
	}

	return handleOk.discardResults()
}

func (mc *mysqlConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	return mc.query(query, args)
}

func (mc *mysqlConn) query(query string, args []driver.Value) (*textRows, error) {
	handleOk := mc.clearResult()

	if mc.closed.Load() {
		return nil, driver.ErrBadConn
	}
	if len(args) != 0 {
		if !mc.cfg.InterpolateParams {
			return nil, driver.ErrSkip
		}
		// try client-side prepare to reduce roundtrip
		prepared, err := mc.interpolateParams(query, args)
		if err != nil {
			return nil, err
		}
		query = prepared
	}
	// Send command
	err := mc.writeCommandPacketStr(comQuery, query)
	if err != nil {
		return nil, mc.markBadConn(err)
	}

	// Read Result
	var resLen int
	resLen, _, err = handleOk.readResultSetHeaderPacket()
	if err != nil {
		return nil, err
	}

	rows := new(textRows)
	rows.mc = mc

	if resLen == 0 {
		rows.rs.done = true

		switch err := rows.NextResultSet(); err {
		case nil, io.EOF:
			return rows, nil
		default:
			return nil, err
		}
	}

	// Columns
	rows.rs.columns, err = mc.readColumns(resLen, nil)
	return rows, err
}

// Gets the value of the given MySQL System Variable
func (mc *mysqlConn) getSystemVar(name string) (string, error) {
	// Send command
	handleOk := mc.clearResult()
	if err := mc.writeCommandPacketStr(comQuery, "SELECT @@"+name); err != nil {
		return "", err
	}

	// Read Result
	resLen, _, err := handleOk.readResultSetHeaderPacket()
	if err == nil {
		rows := new(textRows)
		rows.mc = mc
		rows.rs.columns = []mysqlField{{fieldType: fieldTypeVarChar}}

		if resLen > 0 {
			// Columns
			if err := mc.skipColumns(resLen); err != nil {
				return "", err
			}
		}

		dest := make([]driver.Value, resLen)
		if err = rows.readRow(dest); err == nil {
			// Convert to string before skipRows, which may
			// overwrite the read buffer that dest[0] points into.
			val := string(dest[0].([]byte))
			return val, mc.skipRows()
		}
	}
	return "", err
}

// cancel is called when the query has canceled.
func (mc *mysqlConn) cancel(err error) {
	mc.canceled.Set(err)
	mc.cleanup()
}

// finish is called when the query has succeeded.
func (mc *mysqlConn) finish() {
	if !mc.watching || mc.finished == nil {
		return
	}
	select {
	case mc.finished <- struct{}{}:
		mc.watching = false
	case <-mc.closech:
	}
}

// Ping implements driver.Pinger interface
func (mc *mysqlConn) Ping(ctx context.Context) (err error) {
	if mc.closed.Load() {
		return driver.ErrBadConn
	}

	if err = mc.watchCancel(ctx); err != nil {
		return
	}
	defer mc.finish()

	handleOk := mc.clearResult()
	if err = mc.writeCommandPacket(comPing); err != nil {
		return mc.markBadConn(err)
	}

	return handleOk.readResultOK()
}

// BeginTx implements driver.ConnBeginTx interface
func (mc *mysqlConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if mc.closed.Load() {
		return nil, driver.ErrBadConn
	}

	if err := mc.watchCancel(ctx); err != nil {
		return nil, err
	}
	defer mc.finish()

	if sql.IsolationLevel(opts.Isolation) != sql.LevelDefault {
		level, err := mapIsolationLevel(opts.Isolation)
		if err != nil {
			return nil, err
		}
		err = mc.exec("SET TRANSACTION ISOLATION LEVEL " + level)
		if err != nil {
			return nil, err
		}
	}

	return mc.begin(opts.ReadOnly)
}

func (mc *mysqlConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}

	if err := mc.watchCancel(ctx); err != nil {
		return nil, err
	}

	rows, err := mc.query(query, dargs)
	if err != nil {
		mc.finish()
		return nil, err
	}
	rows.finish = mc.finish
	return rows, err
}

func (mc *mysqlConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}

	if err := mc.watchCancel(ctx); err != nil {
		return nil, err
	}
	defer mc.finish()

	return mc.Exec(query, dargs)
}

func (mc *mysqlConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := mc.watchCancel(ctx); err != nil {
		return nil, err
	}

	stmt, err := mc.Prepare(query)
	mc.finish()
	if err != nil {
		return nil, err
	}

	select {
	default:
	case <-ctx.Done():
		stmt.Close()
		return nil, ctx.Err()
	}
	return stmt, nil
}

func (stmt *mysqlStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}

	if err := stmt.mc.watchCancel(ctx); err != nil {
		return nil, err
	}

	rows, err := stmt.query(dargs)
	if err != nil {
		stmt.mc.finish()
		return nil, err
	}
	rows.finish = stmt.mc.finish
	return rows, err
}

func (stmt *mysqlStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}

	if err := stmt.mc.watchCancel(ctx); err != nil {
		return nil, err
	}
	defer stmt.mc.finish()

	return stmt.Exec(dargs)
}

func (mc *mysqlConn) watchCancel(ctx context.Context) error {
	if mc.watching {
		// Reach here if canceled,
		// so the connection is already invalid
		mc.cleanup()
		return nil
	}
	// When ctx is already cancelled, don't watch it.
	if err := ctx.Err(); err != nil {
		return err
	}
	// When ctx is not cancellable, don't watch it.
	if ctx.Done() == nil {
		return nil
	}
	// When watcher is not alive, can't watch it.
	if mc.watcher == nil {
		return nil
	}

	mc.watching = true
	mc.watcher <- ctx
	return nil
}

func (mc *mysqlConn) startWatcher() {
	watcher := make(chan context.Context, 1)
	mc.watcher = watcher
	finished := make(chan struct{})
	mc.finished = finished
	go func() {
		for {
			var ctx context.Context
			select {
			case ctx = <-watcher:
			case <-mc.closech:
				return
			}

			select {
			case <-ctx.Done():
				mc.cancel(ctx.Err())
			case <-finished:
			case <-mc.closech:
				return
			}
		}
	}()
}

func (mc *mysqlConn) CheckNamedValue(nv *driver.NamedValue) (err error) {
	nv.Value, err = converter{}.ConvertValue(nv.Value)
	return
}

// ResetSession implements driver.SessionResetter.
// (From Go 1.10)
func (mc *mysqlConn) ResetSession(ctx context.Context) error {
	if mc.closed.Load() || mc.buf.busy() {
		return driver.ErrBadConn
	}

	// Perform a stale connection check. We only perform this check for
	// the first query on a connection that has been checked out of the
	// connection pool: a fresh connection from the pool is more likely
	// to be stale, and it has not performed any previous writes that
	// could cause data corruption, so it's safe to return ErrBadConn
	// if the check fails.
	if mc.cfg.CheckConnLiveness {
		conn := mc.netConn
		if mc.rawConn != nil {
			conn = mc.rawConn
		}
		var err error
		if mc.cfg.ReadTimeout != 0 {
			err = conn.SetReadDeadline(time.Now().Add(mc.cfg.ReadTimeout))
		}
		if err == nil {
			err = connCheck(conn)
		}
		if err != nil {
			mc.log("closing bad idle connection: ", err)
			return driver.ErrBadConn
		}
	}

	return nil
}

// IsValid implements driver.Validator interface
// (From Go 1.15)
func (mc *mysqlConn) IsValid() bool {
	return !mc.closed.Load() && !mc.buf.busy()
}

var _ driver.SessionResetter = &mysqlConn{}
var _ driver.Validator = &mysqlConn{}
