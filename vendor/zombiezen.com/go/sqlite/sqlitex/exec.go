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

package sqlitex

import (
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"

	"zombiezen.com/go/sqlite"
)

// ExecOptions is the set of optional arguments executing a statement.
type ExecOptions struct {
	// Args is the set of positional arguments to bind to the statement.
	// The first element in the slice is ?1.
	// See https://sqlite.org/lang_expr.html for more details.
	//
	// Basic reflection on Args is used to map:
	//
	//	integers to BindInt64
	//	floats   to BindFloat
	//	[]byte   to BindBytes
	//	string   to BindText
	//	bool     to BindBool
	//
	// All other kinds are printed using fmt.Sprint(v) and passed to BindText.
	Args []any

	// Named is the set of named arguments to bind to the statement. Keys must
	// start with ':', '@', or '$'. See https://sqlite.org/lang_expr.html for more
	// details.
	//
	// Basic reflection on Named is used to map:
	//
	//	integers to BindInt64
	//	floats   to BindFloat
	//	[]byte   to BindBytes
	//	string   to BindText
	//	bool     to BindBool
	//
	// All other kinds are printed using fmt.Sprint(v) and passed to BindText.
	Named map[string]any

	// ResultFunc is called for each result row.
	// If ResultFunc returns an error then iteration ceases
	// and the execution function returns the error value.
	ResultFunc func(stmt *sqlite.Stmt) error
}

// Exec executes an SQLite query.
//
// For each result row, the resultFn is called.
// Result values can be read by resultFn using stmt.Column* methods.
// If resultFn returns an error then iteration ceases and Exec returns
// the error value.
//
// Any args provided to Exec are bound to numbered parameters of the
// query using the [sqlite.Stmt] Bind* methods. Basic reflection on args is used
// to map:
//
//	integers to BindInt64
//	floats   to BindFloat
//	[]byte   to BindBytes
//	string   to BindText
//	bool     to BindBool
//
// All other kinds are printed using fmt.Sprintf("%v", v) and passed
// to BindText.
//
// Exec is implemented using the Stmt prepare mechanism which allows
// better interactions with Go's type system and avoids pitfalls of
// passing a Go closure to cgo.
//
// As Exec is implemented using Conn.Prepare, subsequent calls to Exec
// with the same statement will reuse the cached statement object.
//
// Deprecated: Use [Execute].
// Exec skips some argument checks for compatibility with crawshaw.io/sqlite.
func Exec(conn *sqlite.Conn, query string, resultFn func(stmt *sqlite.Stmt) error, args ...any) error {
	stmt, err := conn.Prepare(query)
	if err != nil {
		return err
	}
	err = exec(stmt, 0, &ExecOptions{
		Args:       args,
		ResultFunc: resultFn,
	})
	resetErr := stmt.Reset()
	if err == nil {
		err = resetErr
	}
	return err
}

// Execute executes an SQLite query.
//
// As Execute is implemented using [sqlite.Conn.Prepare],
// subsequent calls to Execute with the same statement
// will reuse the cached statement object.
func Execute(conn *sqlite.Conn, query string, opts *ExecOptions) error {
	stmt, err := conn.Prepare(query)
	if err != nil {
		return err
	}
	err = exec(stmt, forbidMissing|forbidExtra, opts)
	resetErr := stmt.Reset()
	if err == nil {
		err = resetErr
	}
	return err
}

// ExecFS is an alias for [ExecuteFS].
//
// Deprecated: Call [ExecuteFS] directly.
func ExecFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) error {
	return ExecuteFS(conn, fsys, filename, opts)
}

// ExecuteFS executes the single statement in the given SQL file.
// ExecuteFS is implemented using [sqlite.Conn.Prepare],
// so subsequent calls to ExecuteFS with the same statement
// will reuse the cached statement object.
func ExecuteFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) error {
	query, err := readString(fsys, filename)
	if err != nil {
		return fmt.Errorf("sqlitex: execute: %w", err)
	}

	stmt, err := conn.Prepare(strings.TrimSpace(query))
	if err != nil {
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	err = exec(stmt, forbidMissing|forbidExtra, opts)
	resetErr := stmt.Reset()
	if err != nil {
		// Don't strip the error query: we already do this inside exec.
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	if resetErr != nil {
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	return nil
}

// ExecTransient executes an SQLite query without caching the underlying query.
// The interface is exactly the same as [Exec].
// It is the spiritual equivalent of sqlite3_exec.
//
// Deprecated: Use [ExecuteTransient].
// ExecTransient skips some argument checks for compatibility with crawshaw.io/sqlite.
func ExecTransient(conn *sqlite.Conn, query string, resultFn func(stmt *sqlite.Stmt) error, args ...any) (err error) {
	var stmt *sqlite.Stmt
	var trailingBytes int
	stmt, trailingBytes, err = conn.PrepareTransient(query)
	if err != nil {
		return err
	}
	defer func() {
		ferr := stmt.Finalize()
		if err == nil {
			err = ferr
		}
	}()
	if trailingBytes != 0 {
		return fmt.Errorf("sqlitex: execute: query %q has trailing bytes", query)
	}
	return exec(stmt, 0, &ExecOptions{
		Args:       args,
		ResultFunc: resultFn,
	})
}

// ExecuteTransient executes an SQLite query without caching the underlying query.
// It is the spiritual equivalent of sqlite3_exec:
// https://www.sqlite.org/c3ref/exec.html
func ExecuteTransient(conn *sqlite.Conn, query string, opts *ExecOptions) (err error) {
	var stmt *sqlite.Stmt
	var trailingBytes int
	stmt, trailingBytes, err = conn.PrepareTransient(query)
	if err != nil {
		return err
	}
	defer func() {
		ferr := stmt.Finalize()
		if err == nil {
			err = ferr
		}
	}()
	if trailingBytes != 0 {
		return fmt.Errorf("sqlitex: execute: query %q has trailing bytes", query)
	}
	return exec(stmt, forbidMissing|forbidExtra, opts)
}

// ExecTransientFS is an alias for [ExecuteTransientFS].
//
// Deprecated: Call [ExecuteTransientFS] directly.
func ExecTransientFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) error {
	return ExecuteTransientFS(conn, fsys, filename, opts)
}

// ExecuteTransientFS executes the single statement in the given SQL file without
// caching the underlying query.
func ExecuteTransientFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) error {
	query, err := readString(fsys, filename)
	if err != nil {
		return fmt.Errorf("sqlitex: execute: %w", err)
	}

	stmt, _, err := conn.PrepareTransient(strings.TrimSpace(query))
	if err != nil {
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	defer stmt.Finalize()
	err = exec(stmt, forbidMissing|forbidExtra, opts)
	resetErr := stmt.Reset()
	if err != nil {
		// Don't strip the error query: we already do this inside exec.
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	if resetErr != nil {
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	return nil
}

// PrepareTransientFS prepares an SQL statement from a file
// that is not cached by the Conn.
// Subsequent calls with the same query will create new Stmts.
// The caller is responsible for calling [sqlite.Stmt.Finalize] on the returned Stmt
// when the Stmt is no longer needed.
func PrepareTransientFS(conn *sqlite.Conn, fsys fs.FS, filename string) (*sqlite.Stmt, error) {
	query, err := readString(fsys, filename)
	if err != nil {
		return nil, fmt.Errorf("sqlitex: prepare: %w", err)
	}
	stmt, _, err := conn.PrepareTransient(strings.TrimSpace(query))
	if err != nil {
		return nil, fmt.Errorf("sqlitex: prepare %s: %w", filename, err)
	}
	return stmt, nil
}

const (
	forbidMissing = 1 << iota
	forbidExtra
)

func exec(stmt *sqlite.Stmt, flags uint8, opts *ExecOptions) (err error) {
	paramCount := stmt.BindParamCount()
	provided := newBitset(paramCount)
	if opts != nil {
		if len(opts.Args) > paramCount {
			return fmt.Errorf("sqlitex: %w (len(Args) > BindParamCount(); %d > %d)",
				sqlite.ResultRange.ToError(), len(opts.Args), paramCount)
		}
		for i, arg := range opts.Args {
			provided.set(i)
			setArg(stmt, i+1, reflect.ValueOf(arg))
		}
		if err := setNamed(stmt, provided, flags, opts.Named); err != nil {
			return err
		}
	}
	if flags&forbidMissing != 0 && !provided.hasAll(paramCount) {
		i := provided.firstMissing() + 1
		name := stmt.BindParamName(i)
		if name == "" {
			name = fmt.Sprintf("?%d", i)
		}
		return fmt.Errorf("sqlitex: missing argument for %s", name)
	}
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return err
		}
		if !hasRow {
			break
		}
		if opts != nil && opts.ResultFunc != nil {
			if err := opts.ResultFunc(stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func setArg(stmt *sqlite.Stmt, i int, v reflect.Value) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		stmt.BindInt64(i, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		stmt.BindInt64(i, int64(v.Uint()))
	case reflect.Float32, reflect.Float64:
		stmt.BindFloat(i, v.Float())
	case reflect.String:
		stmt.BindText(i, v.String())
	case reflect.Bool:
		stmt.BindBool(i, v.Bool())
	case reflect.Invalid:
		stmt.BindNull(i)
	default:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			stmt.BindBytes(i, v.Bytes())
		} else {
			stmt.BindText(i, fmt.Sprint(v.Interface()))
		}
	}
}

func setNamed(stmt *sqlite.Stmt, provided bitset, flags uint8, args map[string]any) error {
	if len(args) == 0 {
		return nil
	}
	var unused map[string]struct{}
	if flags&forbidExtra != 0 {
		unused = make(map[string]struct{}, len(args))
		for k := range args {
			unused[k] = struct{}{}
		}
	}
	for i, count := 1, stmt.BindParamCount(); i <= count; i++ {
		name := stmt.BindParamName(i)
		if name == "" {
			continue
		}
		arg, present := args[name]
		if !present {
			if flags&forbidMissing != 0 {
				// TODO(maybe): Check provided as well?
				return fmt.Errorf("missing parameter %s", name)
			}
			continue
		}
		delete(unused, name)
		provided.set(i - 1)
		setArg(stmt, i, reflect.ValueOf(arg))
	}
	if len(unused) > 0 {
		return fmt.Errorf("%w: unknown argument %s", sqlite.ResultRange.ToError(), minStringInSet(unused))
	}
	return nil
}

// ExecScript executes a script of SQL statements.
// It is the same as calling [ExecuteScript] without options.
func ExecScript(conn *sqlite.Conn, queries string) (err error) {
	return ExecuteScript(conn, queries, nil)
}

// ExecuteScript executes a script of SQL statements.
// The script is wrapped in a SAVEPOINT transaction,
// which is rolled back on any error.
//
// opts.ResultFunc is ignored.
func ExecuteScript(conn *sqlite.Conn, queries string, opts *ExecOptions) (err error) {
	defer Save(conn)(&err)

	unused := make(map[string]struct{})
	if opts != nil {
		for k := range opts.Named {
			unused[k] = struct{}{}
		}
	}
	for {
		queries = strings.TrimSpace(queries)
		if queries == "" {
			break
		}
		var stmt *sqlite.Stmt
		var trailingBytes int
		stmt, trailingBytes, err = conn.PrepareTransient(queries)
		if err != nil {
			return err
		}
		for i, n := 1, stmt.BindParamCount(); i <= n; i++ {
			if name := stmt.BindParamName(i); name != "" {
				delete(unused, name)
			}
		}
		usedBytes := len(queries) - trailingBytes
		queries = queries[usedBytes:]
		err = exec(stmt, forbidMissing, opts)
		stmt.Finalize()
		if err != nil {
			return err
		}
	}
	if len(unused) > 0 {
		return fmt.Errorf("%w: unknown argument %s", sqlite.ResultRange.ToError(), minStringInSet(unused))
	}
	return nil
}

// ExecScriptFS is an alias for [ExecuteScriptFS].
//
// Deprecated: Call [ExecuteScriptFS] directly.
func ExecScriptFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) (err error) {
	return ExecuteScriptFS(conn, fsys, filename, opts)
}

// ExecuteScriptFS executes a script of SQL statements from a file.
// The script is wrapped in a SAVEPOINT transaction,
// which is rolled back on any error.
func ExecuteScriptFS(conn *sqlite.Conn, fsys fs.FS, filename string, opts *ExecOptions) (err error) {
	queries, err := readString(fsys, filename)
	if err != nil {
		return fmt.Errorf("sqlitex: execute script: %w", err)
	}
	if err := ExecuteScript(conn, queries, opts); err != nil {
		return fmt.Errorf("sqlitex: execute %s: %w", filename, err)
	}
	return nil
}

type bitset []uint64

func newBitset(n int) bitset {
	return make([]uint64, (n+63)/64)
}

// hasAll reports whether the bitset is a superset of [0, n).
func (bs bitset) hasAll(n int) bool {
	nbytes := (n + 63) / 64
	if len(bs) < nbytes {
		return false
	}
	fullBytes := n / 64
	for _, b := range bs[:fullBytes] {
		if b != ^uint64(0) {
			return false
		}
	}
	if fullBytes == nbytes {
		return true
	}
	mask := uint64(1)<<(n%64) - 1
	return bs[nbytes-1]&mask == mask
}

func (bs bitset) firstMissing() int {
	for i, b := range bs {
		if b == ^uint64(0) {
			continue
		}
		for j := 0; j < 64; j++ {
			if b&(1<<j) == 0 {
				return i*64 + j
			}
		}
	}
	return len(bs) * 64
}

func (bs bitset) set(n int) {
	bs[n/64] |= 1 << (n % 64)
}

func (bs bitset) String() string {
	sb := new(strings.Builder)
	for i := len(bs) - 1; i >= 0; i-- {
		fmt.Fprintf(sb, "%08b", bs[i])
	}
	return sb.String()
}

func minStringInSet(set map[string]struct{}) string {
	min := ""
	for k := range set {
		if min == "" || k < min {
			min = k
		}
	}
	return min
}

func readString(fsys fs.FS, filename string) (string, error) {
	f, err := fsys.Open(filename)
	if err != nil {
		return "", err
	}
	content := new(strings.Builder)
	_, err = io.Copy(content, f)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("%s: %w", filename, err)
	}
	return content.String(), nil
}
