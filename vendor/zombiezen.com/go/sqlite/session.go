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
	"fmt"
	"io"
	"runtime"
	"sync"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	lib "modernc.org/sqlite/lib"
)

// A Session tracks database changes made by a Conn.
// It is used to build changesets.
//
// For more details: https://www.sqlite.org/sessionintro.html
type Session struct {
	tls *libc.TLS
	ptr uintptr
}

// CreateSession creates a new session object.
// If db is "", then a default of "main" is used.
// It is the caller's responsibility to call Delete when the session is
// no longer needed.
//
// https://www.sqlite.org/session/sqlite3session_create.html
func (c *Conn) CreateSession(db string) (*Session, error) {
	if c == nil {
		return nil, fmt.Errorf("sqlite: create session: nil connection")
	}
	var cdb uintptr
	if db == "" || db == "main" {
		cdb = mainCString
	} else {
		var err error
		cdb, err = libc.CString(db)
		if err != nil {
			return nil, fmt.Errorf("sqlite: create session: %w", err)
		}
		defer libc.Xfree(c.tls, cdb)
	}
	doublePtr, err := malloc(c.tls, ptrSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: create session: %w", err)
	}
	defer libc.Xfree(c.tls, doublePtr)
	res := ResultCode(lib.Xsqlite3session_create(c.tls, c.conn, cdb, doublePtr))
	if err := res.ToError(); err != nil {
		return nil, fmt.Errorf("sqlite: create session: %w", err)
	}
	s := &Session{
		tls: c.tls,
		ptr: *(*uintptr)(unsafe.Pointer(doublePtr)),
	}
	runtime.SetFinalizer(s, func(s *Session) {
		if s.ptr != 0 {
			panic("open *sqlite.Session garbage collected, call Delete method")
		}
	})
	return s, nil
}

// Delete releases any resources associated with the session.
// It must be called before closing the Conn the session is attached to.
func (s *Session) Delete() {
	if s.ptr == 0 {
		panic("Session.Delete called twice on same session")
	}
	lib.Xsqlite3session_delete(s.tls, s.ptr)
	s.ptr = 0
	s.tls = nil
}

// Enable enables recording of changes after a previous call to Disable.
// New sessions start enabled.
//
// https://www.sqlite.org/session/sqlite3session_enable.html
func (s *Session) Enable() {
	if s.ptr == 0 {
		panic("Session.Enable called on deleted session")
	}
	lib.Xsqlite3session_enable(s.tls, s.ptr, 1)
}

// Disable disables recording of changes.
//
// https://www.sqlite.org/session/sqlite3session_enable.html
func (s *Session) Disable() {
	if s.ptr == 0 {
		panic("Session.Disable called on deleted session")
	}
	lib.Xsqlite3session_enable(s.tls, s.ptr, 0)
}

// Attach attaches a table to the session object.
// Changes made to the table will be tracked by the session.
// An empty tableName attaches all the tables in the database.
func (s *Session) Attach(tableName string) error {
	if s.ptr == 0 {
		return fmt.Errorf("sqlite: attach table %q to session: session deleted", tableName)
	}
	var ctable uintptr
	if tableName != "" {
		var err error
		ctable, err = libc.CString(tableName)
		if err != nil {
			return fmt.Errorf("sqlite: attach table %q to session: %v", tableName, err)
		}
		defer libc.Xfree(s.tls, ctable)
	}
	res := ResultCode(lib.Xsqlite3session_attach(s.tls, s.ptr, ctable))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: attach table %q to session: %w", tableName, err)
	}
	return nil
}

// Diff appends the difference between two tables (srcDB and the session DB) to
// the session. The two tables must have the same name and schema.
//
// https://www.sqlite.org/session/sqlite3session_diff.html
func (s *Session) Diff(srcDB, tableName string) error {
	if s.ptr == 0 {
		return fmt.Errorf("sqlite: diff table %q: session deleted", tableName)
	}
	errMsgPtr, err := malloc(s.tls, ptrSize)
	if err != nil {
		return fmt.Errorf("sqlite: diff table %q: %v", tableName, err)
	}
	defer libc.Xfree(s.tls, errMsgPtr)
	csrcDB, err := libc.CString(srcDB)
	if err != nil {
		return fmt.Errorf("sqlite: diff table %q: %v", tableName, err)
	}
	defer libc.Xfree(s.tls, csrcDB)
	ctable, err := libc.CString(tableName)
	if err != nil {
		return fmt.Errorf("sqlite: diff table %q: %v", tableName, err)
	}
	defer libc.Xfree(s.tls, ctable)
	res := ResultCode(lib.Xsqlite3session_diff(s.tls, s.ptr, csrcDB, ctable, errMsgPtr))
	if err := res.ToError(); err != nil {
		cerrMsg := *(*uintptr)(unsafe.Pointer(errMsgPtr))
		if cerrMsg == 0 {
			return fmt.Errorf("sqlite: diff table %q: %w", tableName, err)
		}
		errMsg := libc.GoString(cerrMsg)
		lib.Xsqlite3_free(s.tls, cerrMsg)
		return fmt.Errorf("sqlite: diff table %q: %w (%s)", tableName, err, errMsg)
	}
	return nil
}

// WriteChangeset generates a changeset from a session.
//
// https://www.sqlite.org/session/sqlite3session_changeset.html
func (s *Session) WriteChangeset(w io.Writer) error {
	if s.ptr == 0 {
		return fmt.Errorf("sqlite: write session changeset: session deleted")
	}
	xOutput, pOut := registerStreamWriter(w)
	defer unregisterStreamWriter(pOut)
	res := ResultCode(lib.Xsqlite3session_changeset_strm(s.tls, s.ptr, xOutput, pOut))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: write session changeset: %w", err)
	}
	return nil
}

// WritePatchset generates a patchset from a session.
//
// https://www.sqlite.org/session/sqlite3session_patchset.html
func (s *Session) WritePatchset(w io.Writer) error {
	if s.ptr == 0 {
		return fmt.Errorf("sqlite: write session patchset: session deleted")
	}
	xOutput, pOut := registerStreamWriter(w)
	defer unregisterStreamWriter(pOut)
	res := ResultCode(lib.Xsqlite3session_patchset_strm(s.tls, s.ptr, xOutput, pOut))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: write session patchset: %w", err)
	}
	return nil
}

// ApplyChangeset applies a changeset to the database.
//
// If filterFn is not nil and the changeset includes changes for a table for
// which the function reports false, then the changes are ignored. If filterFn
// is nil, then all changes are applied.
//
// If a changeset will not apply cleanly, then conflictFn will be called to
// resolve the conflict. See https://www.sqlite.org/session/sqlite3changeset_apply.html
// for more details.
func (c *Conn) ApplyChangeset(r io.Reader, filterFn func(tableName string) bool, conflictFn ConflictHandler) error {
	if c == nil {
		return fmt.Errorf("sqlite: apply changeset: nil connection")
	}
	if conflictFn == nil {
		return fmt.Errorf("sqlite: apply changeset: no conflict handler provided")
	}
	xInput, pIn := registerStreamReader(r)
	defer unregisterStreamReader(pIn)
	appliesIDMu.Lock()
	pCtx := appliesIDs.next()
	appliesIDMu.Unlock()
	applies.Store(pCtx, applyFuncs{
		tls:        c.tls,
		filterFn:   filterFn,
		conflictFn: conflictFn,
	})
	defer func() {
		applies.Delete(pCtx)
		appliesIDMu.Lock()
		appliesIDs.reclaim(pCtx)
		appliesIDMu.Unlock()
	}()

	xFilter := uintptr(0)
	if filterFn != nil {
		xFilter = cFuncPointer(changesetApplyFilter)
	}
	xConflict := cFuncPointer(changesetApplyConflict)
	res := ResultCode(lib.Xsqlite3changeset_apply_strm(c.tls, c.conn, xInput, pIn, xFilter, xConflict, pCtx))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: apply changeset: %w", err)
	}
	return nil
}

type applyFuncs struct {
	tls        *libc.TLS
	filterFn   func(tableName string) bool
	conflictFn ConflictHandler
}

var (
	applies sync.Map // map[uintptr]applyFuncs

	appliesIDMu sync.Mutex
	appliesIDs  idGen
)

func changesetApplyFilter(tls *libc.TLS, pCtx uintptr, zTab uintptr) int32 {
	appliesValue, _ := applies.Load(pCtx)
	funcs := appliesValue.(applyFuncs)
	tableName := libc.GoString(zTab)
	if funcs.filterFn(tableName) {
		return 1
	} else {
		return 0
	}
}

func changesetApplyConflict(tls *libc.TLS, pCtx uintptr, eConflict int32, p uintptr) int32 {
	appliesValue, _ := applies.Load(pCtx)
	funcs := appliesValue.(applyFuncs)
	return int32(funcs.conflictFn(ConflictType(eConflict), &ChangesetIterator{
		tls: tls,
		ptr: p,
	}))
}

// ApplyInverseChangeset applies the inverse of a changeset to the database.
// See ApplyChangeset and InvertChangeset for more details.
func (c *Conn) ApplyInverseChangeset(r io.Reader, filterFn func(tableName string) bool, conflictFn ConflictHandler) error {
	if c == nil {
		return fmt.Errorf("sqlite: apply changeset: nil connection")
	}
	pr, pw := io.Pipe()
	go func() {
		err := InvertChangeset(pw, pr)
		pw.CloseWithError(err)
	}()
	err := c.ApplyChangeset(pr, filterFn, conflictFn)
	io.Copy(io.Discard, pr) // wait for invert goroutine to finish
	return err
}

// InvertChangeset generates an inverted changeset. Applying an inverted
// changeset to a database reverses the effects of applying the uninverted
// changeset.
//
// This function currently assumes that the input is a valid changeset.
// If it is not, the results are undefined.
//
// https://www.sqlite.org/session/sqlite3changeset_invert.html
func InvertChangeset(w io.Writer, r io.Reader) error {
	tls := libc.NewTLS()
	defer tls.Close()
	initlib(tls)
	xInput, pIn := registerStreamReader(r)
	defer unregisterStreamReader(pIn)
	xOutput, pOut := registerStreamWriter(w)
	defer unregisterStreamWriter(pOut)
	res := ResultCode(lib.Xsqlite3changeset_invert_strm(tls, xInput, pIn, xOutput, pOut))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: invert changeset: %w", err)
	}
	return nil
}

// ChangesetIterator is an iterator over a changeset.
type ChangesetIterator struct {
	tls    *libc.TLS
	ptr    uintptr
	ownTLS bool
	pIn    uintptr // if non-zero, then must be unregistered at Finalize
}

// NewChangesetIterator returns a new iterator over the contents of the
// changeset. The caller is responsible for calling Finalize on the returned
// iterator.
//
// https://www.sqlite.org/session/sqlite3changeset_start.html
func NewChangesetIterator(r io.Reader) (*ChangesetIterator, error) {
	tls := libc.NewTLS()
	initlib(tls)
	xInput, pIn := registerStreamReader(r)
	pp, err := malloc(tls, ptrSize)
	if err != nil {
		unregisterStreamReader(pIn)
		tls.Close()
		return nil, fmt.Errorf("sqlite: start changeset iterator: %v", err)
	}
	defer libc.Xfree(tls, pp)
	res := ResultCode(lib.Xsqlite3changeset_start_strm(tls, pp, xInput, pIn))
	if err := res.ToError(); err != nil {
		unregisterStreamReader(pIn)
		tls.Close()
		return nil, fmt.Errorf("sqlite: start changeset iterator: %w", err)
	}
	iter := &ChangesetIterator{
		tls:    tls,
		ownTLS: true,
		ptr:    *(*uintptr)(unsafe.Pointer(pp)),
		pIn:    pIn,
	}
	runtime.SetFinalizer(iter, func(iter *ChangesetIterator) {
		if iter.ptr != 0 {
			panic("open *sqlite.ChangesetIterator garbage collected, call Finalize method")
		}
	})
	return iter, nil
}

// Close releases any resources associated with the iterator created with
// NewChangesetIterator.
func (iter *ChangesetIterator) Close() error {
	if iter.ptr == 0 {
		return fmt.Errorf("sqlite: finalize changeset iterator: called twice on same iterator")
	}
	res := ResultCode(lib.Xsqlite3changeset_finalize(iter.tls, iter.ptr))
	iter.ptr = 0
	if iter.ownTLS {
		iter.tls.Close()
	}
	iter.tls = nil
	if iter.pIn != 0 {
		unregisterStreamReader(iter.pIn)
	}
	iter.pIn = 0
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: finalize changeset iterator: %w", err)
	}
	return nil
}

// Next advances the iterator to the next change in the changeset.
// It is an error to call Next on an iterator passed to an ApplyChangeset
// conflict handler.
//
// https://www.sqlite.org/session/sqlite3changeset_next.html
func (iter *ChangesetIterator) Next() (rowReturned bool, err error) {
	res := ResultCode(lib.Xsqlite3changeset_next(iter.tls, iter.ptr))
	switch res {
	case ResultRow:
		return true, nil
	case ResultDone:
		return false, nil
	default:
		return false, fmt.Errorf("sqlite: iterate changeset: %w", res.ToError())
	}
}

// ChangesetOperation holds information about a change in a changeset.
type ChangesetOperation struct {
	// Type is one of OpInsert, OpDelete, or OpUpdate.
	Type OpType
	// TableName is the name of the table affected by the change.
	TableName string
	// NumColumns is the number of columns in the table affected by the change.
	NumColumns int
	// Indirect is true if the session object "indirect" flag was set when the
	// change was made or the change was made by an SQL trigger or foreign key
	// action instead of directly as a result of a users SQL statement.
	Indirect bool
}

// Operation obtains the current operation from the iterator.
//
// https://www.sqlite.org/session/sqlite3changeset_op.html
func (iter *ChangesetIterator) Operation() (*ChangesetOperation, error) {
	if iter.ptr == 0 {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: iterator finalized")
	}
	pzTab, err := malloc(iter.tls, ptrSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: %v", err)
	}
	defer libc.Xfree(iter.tls, pzTab)
	pnCol, err := malloc(iter.tls, types.Size_t(unsafe.Sizeof(int32(0))))
	if err != nil {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: %v", err)
	}
	defer libc.Xfree(iter.tls, pnCol)
	pOp, err := malloc(iter.tls, types.Size_t(unsafe.Sizeof(int32(0))))
	if err != nil {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: %v", err)
	}
	defer libc.Xfree(iter.tls, pOp)
	pbIndirect, err := malloc(iter.tls, types.Size_t(unsafe.Sizeof(int32(0))))
	if err != nil {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: %v", err)
	}
	defer libc.Xfree(iter.tls, pbIndirect)
	res := ResultCode(lib.Xsqlite3changeset_op(iter.tls, iter.ptr, pzTab, pnCol, pOp, pbIndirect))
	if err := res.ToError(); err != nil {
		return nil, fmt.Errorf("sqlite: changeset iterator operation: %w", err)
	}
	return &ChangesetOperation{
		Type:       OpType(*(*int32)(unsafe.Pointer(pOp))),
		TableName:  libc.GoString(*(*uintptr)(unsafe.Pointer(pzTab))),
		NumColumns: int(*(*int32)(unsafe.Pointer(pnCol))),
		Indirect:   *(*int32)(unsafe.Pointer(pbIndirect)) != 0,
	}, nil
}

// Old obtains the old row value from an iterator. Column indices start at 0.
// The returned value is valid until the iterator is finalized.
//
// https://www.sqlite.org/session/sqlite3changeset_old.html
func (iter *ChangesetIterator) Old(col int) (Value, error) {
	if iter.ptr == 0 {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: iterator finalized")
	}
	ppValue, err := malloc(iter.tls, ptrSize)
	if err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %v", err)
	}
	defer libc.Xfree(iter.tls, ppValue)
	res := ResultCode(lib.Xsqlite3changeset_old(iter.tls, iter.ptr, int32(col), ppValue))
	if err := res.ToError(); err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %w", err)
	}
	return Value{
		tls:       iter.tls,
		ptrOrType: *(*uintptr)(unsafe.Pointer(ppValue)),
	}, nil
}

// New obtains the new row value from an iterator. Column indices start at 0.
// The returned value is valid until the iterator is finalized.
//
// https://www.sqlite.org/session/sqlite3changeset_new.html
func (iter *ChangesetIterator) New(col int) (Value, error) {
	if iter.ptr == 0 {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: iterator finalized")
	}
	ppValue, err := malloc(iter.tls, ptrSize)
	if err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %v", err)
	}
	defer libc.Xfree(iter.tls, ppValue)
	res := ResultCode(lib.Xsqlite3changeset_new(iter.tls, iter.ptr, int32(col), ppValue))
	if err := res.ToError(); err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %w", err)
	}
	return Value{
		tls:       iter.tls,
		ptrOrType: *(*uintptr)(unsafe.Pointer(ppValue)),
	}, nil
}

// ConflictValue obtains the conflicting row value from an iterator.
// Column indices start at 0. The returned value is valid until the iterator is
// finalized.
//
// https://www.sqlite.org/session/sqlite3changeset_conflict.html
func (iter *ChangesetIterator) ConflictValue(col int) (Value, error) {
	if iter.ptr == 0 {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: iterator finalized")
	}
	ppValue, err := malloc(iter.tls, ptrSize)
	if err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %v", err)
	}
	defer libc.Xfree(iter.tls, ppValue)
	res := ResultCode(lib.Xsqlite3changeset_conflict(iter.tls, iter.ptr, int32(col), ppValue))
	if err := res.ToError(); err != nil {
		return Value{}, fmt.Errorf("sqlite: get changeset iterator value: %w", err)
	}
	return Value{
		tls:       iter.tls,
		ptrOrType: *(*uintptr)(unsafe.Pointer(ppValue)),
	}, nil
}

// ForeignKeyConflicts returns the number of foreign key constraint violations.
//
// https://www.sqlite.org/session/sqlite3changeset_fk_conflicts.html
func (iter *ChangesetIterator) ForeignKeyConflicts() (int, error) {
	pnOut, err := malloc(iter.tls, types.Size_t(unsafe.Sizeof(int32(0))))
	if err != nil {
		return 0, fmt.Errorf("sqlite: get number of foreign key conflicts: %v", err)
	}
	defer libc.Xfree(iter.tls, pnOut)
	res := ResultCode(lib.Xsqlite3changeset_fk_conflicts(iter.tls, iter.ptr, pnOut))
	if err := res.ToError(); err != nil {
		return 0, fmt.Errorf("sqlite: get number of foreign key conflicts: %w", err)
	}
	return int(*(*int32)(unsafe.Pointer(pnOut))), nil
}

// PrimaryKey returns a map of columns that make up the primary key.
//
// https://www.sqlite.org/session/sqlite3changeset_pk.html
func (iter *ChangesetIterator) PrimaryKey() ([]bool, error) {
	pabPK, err := malloc(iter.tls, ptrSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: get primary key columns: %v", err)
	}
	defer libc.Xfree(iter.tls, pabPK)
	pnCol, err := malloc(iter.tls, types.Size_t(unsafe.Sizeof(int32(0))))
	if err != nil {
		return nil, fmt.Errorf("sqlite: get primary key columns: %v", err)
	}
	defer libc.Xfree(iter.tls, pnCol)
	res := ResultCode(lib.Xsqlite3changeset_pk(iter.tls, iter.ptr, pabPK, pnCol))
	if err := res.ToError(); err != nil {
		return nil, fmt.Errorf("sqlite: get primary key columns: %w", err)
	}
	c := libc.GoBytes(*(*uintptr)(unsafe.Pointer(pabPK)), int(*(*int32)(unsafe.Pointer(pnCol))))
	cols := make([]bool, len(c))
	for i := range cols {
		cols[i] = c[i] != 0
	}
	return cols, nil
}

// ConcatChangesets concatenates two changesets into a single changeset.
//
// https://www.sqlite.org/session/sqlite3changeset_concat.html
func ConcatChangesets(w io.Writer, changeset1, changeset2 io.Reader) error {
	tls := libc.NewTLS()
	defer tls.Close()
	initlib(tls)
	xInput1, pIn1 := registerStreamReader(changeset1)
	defer unregisterStreamReader(pIn1)
	xInput2, pIn2 := registerStreamReader(changeset2)
	defer unregisterStreamReader(pIn2)
	xOutput, pOut := registerStreamWriter(w)
	defer unregisterStreamWriter(pOut)
	res := ResultCode(lib.Xsqlite3changeset_concat_strm(tls, xInput1, pIn1, xInput2, pIn2, xOutput, pOut))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: concatenate changesets: %w", err)
	}
	return nil
}

// A Changegroup is an object used to combine two or more changesets or
// patchesets. The zero value is an empty changegroup.
//
// https://www.sqlite.org/session/changegroup.html
type Changegroup struct {
	tls *libc.TLS
	ptr uintptr
}

// NewChangegroup returns a new changegroup. The caller is responsible for
// calling Clear on the returned changegroup.
//
// https://www.sqlite.org/session/sqlite3changegroup_new.html
//
// Deprecated: Use new(sqlite.Changegroup) instead, which does not require
// calling Clear until Add is called.
func NewChangegroup() (*Changegroup, error) {
	cg := new(Changegroup)
	if err := cg.init(); err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	return cg, nil
}

func (cg *Changegroup) init() error {
	if cg.tls == nil {
		cg.tls = libc.NewTLS()
	}
	if cg.ptr == 0 {
		pp, err := malloc(cg.tls, ptrSize)
		if err != nil {
			cg.tls.Close()
			cg.tls = nil
			return fmt.Errorf("init changegroup: %v", err)
		}
		defer libc.Xfree(cg.tls, pp)
		initlib(cg.tls)
		res := ResultCode(lib.Xsqlite3changegroup_new(cg.tls, pp))
		if err := res.ToError(); err != nil {
			cg.tls.Close()
			cg.tls = nil
			return fmt.Errorf("init changegroup: %w", err)
		}
		cg.ptr = *(*uintptr)(unsafe.Pointer(pp))
	}
	return nil
}

// Clear empties the changegroup and releases any resources associated with
// the changegroup. This method may be called multiple times.
func (cg *Changegroup) Clear() {
	if cg == nil {
		return
	}
	if cg.ptr != 0 {
		lib.Xsqlite3changegroup_delete(cg.tls, cg.ptr)
		cg.ptr = 0
	}
	if cg.tls != nil {
		cg.tls.Close()
		cg.tls = nil
	}
}

// Add adds all changes within the changeset (or patchset) read from r to
// the changegroup. Once Add has been called, it is the caller's responsibility
// to call Clear.
//
// https://www.sqlite.org/session/sqlite3changegroup_add.html
func (cg *Changegroup) Add(r io.Reader) error {
	if err := cg.init(); err != nil {
		return fmt.Errorf("sqlite: add to changegroup: %w", err)
	}
	xInput, pIn := registerStreamReader(r)
	defer unregisterStreamReader(pIn)
	res := ResultCode(lib.Xsqlite3changegroup_add_strm(cg.tls, cg.ptr, xInput, pIn))
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: add to changegroup: %w", err)
	}
	return nil
}

// WriteTo writes the current contents of the changegroup to w.
//
// https://www.sqlite.org/session/sqlite3changegroup_output.html
func (cg *Changegroup) WriteTo(w io.Writer) (n int64, err error) {
	// We want to allow uninitialized changegroups to write output without
	// forcing the caller to call Clear. In theses cases, we initialize a new
	// changegroup that lasts for the length of the WriteTo call.
	if cg == nil {
		cg = new(Changegroup)
	}
	if cg.ptr == 0 {
		defer cg.Clear()
	}

	if err := cg.init(); err != nil {
		return 0, fmt.Errorf("sqlite: write changegroup: %w", err)
	}
	wc := &writeCounter{Writer: w}
	xOutput, pOut := registerStreamWriter(wc)
	defer unregisterStreamWriter(pOut)
	res := ResultCode(lib.Xsqlite3changegroup_output_strm(cg.tls, cg.ptr, xOutput, pOut))
	if err := res.ToError(); err != nil {
		return wc.n, fmt.Errorf("sqlite: write changegroup: %w", err)
	}
	return wc.n, nil
}

// A ConflictHandler function determines the action to take to resolve a
// conflict while applying a changeset.
//
// https://www.sqlite.org/session/sqlite3changeset_apply.html
type ConflictHandler func(ConflictType, *ChangesetIterator) ConflictAction

// ConflictType is an enumeration of changeset conflict types.
//
// https://www.sqlite.org/session/c_changeset_conflict.html
type ConflictType int32

// Conflict types.
const (
	ChangesetData       = ConflictType(lib.SQLITE_CHANGESET_DATA)
	ChangesetNotFound   = ConflictType(lib.SQLITE_CHANGESET_NOTFOUND)
	ChangesetConflict   = ConflictType(lib.SQLITE_CHANGESET_CONFLICT)
	ChangesetConstraint = ConflictType(lib.SQLITE_CHANGESET_CONSTRAINT)
	ChangesetForeignKey = ConflictType(lib.SQLITE_CHANGESET_FOREIGN_KEY)
)

// String returns the C constant name of the conflict type.
func (code ConflictType) String() string {
	switch code {
	case ChangesetData:
		return "SQLITE_CHANGESET_DATA"
	case ChangesetNotFound:
		return "SQLITE_CHANGESET_NOTFOUND"
	case ChangesetConflict:
		return "SQLITE_CHANGESET_CONFLICT"
	case ChangesetConstraint:
		return "SQLITE_CHANGESET_CONSTRAINT"
	case ChangesetForeignKey:
		return "SQLITE_CHANGESET_FOREIGN_KEY"
	default:
		return fmt.Sprintf("ConflictType(%d)", int32(code))
	}
}

// ConflictAction is an enumeration of actions that can be taken in response to
// a changeset conflict. The zero value is ChangesetOmit.
//
// https://www.sqlite.org/session/c_changeset_abort.html
type ConflictAction int32

// Conflict actions.
const (
	// ChangesetOmit signals that no special action should be taken. The change
	// that caused the conflict will not be applied. The session module continues
	// to the next change in the changeset.
	ChangesetOmit = ConflictAction(lib.SQLITE_CHANGESET_OMIT)
	// ChangesetAbort signals that any changes applied so far should be rolled
	// back and the call to ApplyChangeset returns an error whose code
	// is ResultAbort.
	ChangesetAbort = ConflictAction(lib.SQLITE_CHANGESET_ABORT)
	// ChangesetReplace signals a different action depending on the conflict type.
	//
	// If the conflict type is ChangesetData, ChangesetReplace signals the
	// conflicting row should be updated or deleted.
	//
	// If the conflict type is ChangesetConflict, then ChangesetReplace signals
	// that the conflicting row should be removed from the database and a second
	// attempt to apply the change should be made. If this second attempt fails,
	// the original row is restored to the database before continuing.
	//
	// For all other conflict types, returning ChangesetReplace will cause
	// ApplyChangeset to roll back any changes applied so far and return an error
	// whose code is ResultMisuse.
	ChangesetReplace = ConflictAction(lib.SQLITE_CHANGESET_REPLACE)
)

// String returns the C constant name of the conflict action.
func (code ConflictAction) String() string {
	switch code {
	case ChangesetOmit:
		return "SQLITE_CHANGESET_OMIT"
	case ChangesetAbort:
		return "SQLITE_CHANGESET_ABORT"
	case ChangesetReplace:
		return "SQLITE_CHANGESET_REPLACE"
	default:
		return fmt.Sprintf("ConflictAction(%d)", int32(code))
	}
}

var (
	streamReaders sync.Map // map[uintptr]io.Reader

	streamReadersIDMu sync.Mutex
	streamReadersIDs  idGen
)

func registerStreamReader(r io.Reader) (xInput, pIn uintptr) {
	xInput = cFuncPointer(sessionStreamInput)
	streamReadersIDMu.Lock()
	pIn = streamReadersIDs.next()
	streamReadersIDMu.Unlock()
	streamReaders.Store(pIn, r)
	return
}

func unregisterStreamReader(pIn uintptr) {
	streamReaders.Delete(pIn)

	streamReadersIDMu.Lock()
	streamReadersIDs.reclaim(pIn)
	streamReadersIDMu.Unlock()
}

// sessionStreamInput is the callback returned by registerSessionReader used
// for the session streaming APIs.
// https://www.sqlite.org/session/sqlite3changegroup_add_strm.html
func sessionStreamInput(tls *libc.TLS, pIn uintptr, pData uintptr, pnData uintptr) int32 {
	rval, _ := streamReaders.Load(pIn)
	r, _ := rval.(io.Reader)
	if r == nil {
		return lib.SQLITE_MISUSE
	}
	n := int(*(*int32)(unsafe.Pointer(pnData)))
	n, err := r.Read(libc.GoBytes(pData, n))
	*(*int32)(unsafe.Pointer(pnData)) = int32(n)
	if n == 0 && err != io.EOF {
		// Readers should not return n == 0 && err == nil. However, as per io.Reader
		// docs, we can't treat it as an EOF condition.
		return lib.SQLITE_IOERR_READ
	}
	return lib.SQLITE_OK
}

var (
	streamWriters sync.Map // map[uintptr]io.Writer

	streamWritersIDMu sync.Mutex
	streamWritersIDs  idGen
)

func registerStreamWriter(w io.Writer) (xOutput, pOut uintptr) {
	xOutput = cFuncPointer(sessionStreamOutput)
	streamWritersIDMu.Lock()
	pOut = streamWritersIDs.next()
	streamWritersIDMu.Unlock()
	streamWriters.Store(pOut, w)
	return
}

func unregisterStreamWriter(pOut uintptr) {
	streamWriters.Delete(pOut)

	streamWritersIDMu.Lock()
	streamWritersIDs.reclaim(pOut)
	streamWritersIDMu.Unlock()
}

// sessionStreamOutput is the callback returned by registerSessionWriter used
// for the session streaming APIs.
// https://www.sqlite.org/session/sqlite3changegroup_add_strm.html
func sessionStreamOutput(tls *libc.TLS, pOut uintptr, pData uintptr, nData int32) int32 {
	wval, _ := streamWriters.Load(pOut)
	w, _ := wval.(io.Writer)
	if w == nil {
		return lib.SQLITE_MISUSE
	}
	_, err := w.Write(libc.GoBytes(pData, int(nData)))
	if err != nil {
		return lib.SQLITE_IOERR_WRITE
	}
	return lib.SQLITE_OK
}

type writeCounter struct {
	io.Writer
	n int64
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n, err := wc.Writer.Write(p)
	wc.n += int64(n)
	return n, err
}
