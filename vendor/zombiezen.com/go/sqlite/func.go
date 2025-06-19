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
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	lib "modernc.org/sqlite/lib"
)

var auxdata struct {
	mu  sync.RWMutex
	m   map[uintptr]any
	ids idGen
}

// Context is a SQL function execution context.
// It is in no way related to a Go context.Context.
// https://sqlite.org/c3ref/context.html
type Context struct {
	tls *libc.TLS
	ptr uintptr
}

// Conn returns the database connection that is calling the SQL function.
func (ctx Context) Conn() *Conn {
	connPtr := lib.Xsqlite3_context_db_handle(ctx.tls, ctx.ptr)
	allConns.mu.RLock()
	defer allConns.mu.RUnlock()
	return allConns.table[connPtr]
}

// AuxData returns the auxiliary data associated with the given argument, with
// zero being the leftmost argument, or nil if no such data is present.
//
// Auxiliary data may be used by (non-aggregate) SQL functions to associate
// metadata with argument values. If the same value is passed to multiple
// invocations of the same SQL function during query execution, under some
// circumstances the associated metadata may be preserved. An example of where
// this might be useful is in a regular-expression matching function. The
// compiled version of the regular expression can be stored as metadata
// associated with the pattern string. Then as long as the pattern string
// remains the same, the compiled regular expression can be reused on multiple
// invocations of the same function.
//
// For more details, see https://www.sqlite.org/c3ref/get_auxdata.html
func (ctx Context) AuxData(arg int) any {
	id := lib.Xsqlite3_get_auxdata(ctx.tls, ctx.ptr, int32(arg))
	if id == 0 {
		return nil
	}
	auxdata.mu.RLock()
	defer auxdata.mu.RUnlock()
	return auxdata.m[id]
}

// SetAuxData sets the auxiliary data associated with the given argument, with
// zero being the leftmost argument. SQLite is free to discard the metadata at
// any time, including during the call to SetAuxData.
//
// Auxiliary data may be used by (non-aggregate) SQL functions to associate
// metadata with argument values. If the same value is passed to multiple
// invocations of the same SQL function during query execution, under some
// circumstances the associated metadata may be preserved. An example of where
// this might be useful is in a regular-expression matching function. The
// compiled version of the regular expression can be stored as metadata
// associated with the pattern string. Then as long as the pattern string
// remains the same, the compiled regular expression can be reused on multiple
// invocations of the same function.
//
// For more details, see https://www.sqlite.org/c3ref/get_auxdata.html
func (ctx Context) SetAuxData(arg int, data any) {
	auxdata.mu.Lock()
	id := auxdata.ids.next()
	if auxdata.m == nil {
		auxdata.m = make(map[uintptr]any)
	}
	auxdata.m[id] = data
	auxdata.mu.Unlock()

	deleteFn := cFuncPointer(freeAuxData)

	lib.Xsqlite3_set_auxdata(ctx.tls, ctx.ptr, int32(arg), id, deleteFn)
}

func freeAuxData(tls *libc.TLS, id uintptr) {
	auxdata.mu.Lock()
	defer auxdata.mu.Unlock()
	delete(auxdata.m, id)
	auxdata.ids.reclaim(id)
}

func (ctx Context) result(v Value, err error) {
	if err != nil {
		ctx.resultError(err)
		return
	}
	if v.tls != nil {
		if ctx.tls != v.tls {
			ctx.resultError(fmt.Errorf("function result Value from different connection"))
			return
		}
		lib.Xsqlite3_result_value(ctx.tls, ctx.ptr, v.ptrOrType)
		return
	}
	switch ColumnType(v.ptrOrType) {
	case 0, TypeNull:
		lib.Xsqlite3_result_null(ctx.tls, ctx.ptr)
	case TypeInteger:
		lib.Xsqlite3_result_int64(ctx.tls, ctx.ptr, v.n)
	case TypeFloat:
		lib.Xsqlite3_result_double(ctx.tls, ctx.ptr, v.float())
	case TypeText:
		if len(v.s) == 0 {
			lib.Xsqlite3_result_text(ctx.tls, ctx.ptr, emptyCString, 0, sqliteStatic)
		} else {
			cv, err := libc.CString(v.s)
			if err != nil {
				ctx.resultError(fmt.Errorf("alloc function result: %w", err))
				return
			}
			lib.Xsqlite3_result_text(ctx.tls, ctx.ptr, cv, int32(len(v.s)), freeFuncPtr)
		}
	case TypeBlob:
		if len(v.s) == 0 {
			lib.Xsqlite3_result_blob(ctx.tls, ctx.ptr, emptyCString, 0, sqliteStatic)
		} else {
			cv, err := malloc(ctx.tls, types.Size_t(len(v.s)))
			if err != nil {
				ctx.resultError(fmt.Errorf("alloc function result: %w", err))
				return
			}
			copy(libc.GoBytes(cv, len(v.s)), v.s)
			lib.Xsqlite3_result_blob(ctx.tls, ctx.ptr, cv, int32(len(v.s)), freeFuncPtr)
		}
	default:
		panic("unknown result Value type")
	}
}

func (ctx Context) resultError(err error) {
	errstr := err.Error()
	cerrstr, err := libc.CString(errstr)
	if err != nil {
		panic(err)
	}
	defer libc.Xfree(ctx.tls, cerrstr)
	lib.Xsqlite3_result_error(ctx.tls, ctx.ptr, cerrstr, int32(len(errstr)))
	lib.Xsqlite3_result_error_code(ctx.tls, ctx.ptr, int32(ErrCode(err)))
}

// Value represents a value that can be stored in a database table.
// The zero value is NULL.
// The accessor methods on Value may perform automatic conversions
// and thus methods on Value must not be called concurrently.
type Value struct {
	tls       *libc.TLS
	ptrOrType uintptr // pointer to sqlite_value if tls != nil, ColumnType otherwise

	s string
	n int64 // if ptrOrType == 0 and n != 0, then indicates the "nochange" NULL.
}

// IntegerValue returns a new Value representing the given integer.
func IntegerValue(i int64) Value {
	return Value{ptrOrType: uintptr(TypeInteger), n: i}
}

// FloatValue returns a new Value representing the given floating-point number.
func FloatValue(f float64) Value {
	return Value{ptrOrType: uintptr(TypeFloat), n: int64(math.Float64bits(f))}
}

// TextValue returns a new Value representing the given string.
func TextValue(s string) Value {
	return Value{ptrOrType: uintptr(TypeText), s: s}
}

// BlobValue returns a new blob Value, copying the bytes from the given
// byte slice.
func BlobValue(b []byte) Value {
	return Value{ptrOrType: uintptr(TypeBlob), s: string(b)}
}

// Unchanged returns a NULL Value for which [Value.NoChange] reports true.
// This is only significant as the return value for the Column method of [VTableCursor].
func Unchanged() Value {
	return Value{n: 1}
}

// Type returns the data type of the value. The result of Type is undefined if
// an automatic type conversion has occurred due to calling one of the other
// accessor methods.
func (v Value) Type() ColumnType {
	if v.ptrOrType == 0 {
		return TypeNull
	}
	if v.tls == nil {
		return ColumnType(v.ptrOrType)
	}
	return ColumnType(lib.Xsqlite3_value_type(v.tls, v.ptrOrType))
}

// Conversions follow the table in https://sqlite.org/c3ref/column_blob.html

// Int returns the value as an integer.
func (v Value) Int() int {
	return int(v.Int64())
}

// Int64 returns the value as a 64-bit integer.
func (v Value) Int64() int64 {
	if v.ptrOrType == 0 {
		return 0
	}
	if v.tls == nil {
		switch ColumnType(v.ptrOrType) {
		case TypeNull:
			return 0
		case TypeInteger:
			return v.n
		case TypeFloat:
			return int64(v.float())
		case TypeBlob, TypeText:
			return castTextToInteger(v.s)
		default:
			panic("unknown value type")
		}
	}
	return int64(lib.Xsqlite3_value_int64(v.tls, v.ptrOrType))
}

// castTextToInteger emulates the SQLite CAST operator for a TEXT value to
// INTEGER, as documented in https://sqlite.org/lang_expr.html#castexpr
func castTextToInteger(s string) int64 {
	const digits = "0123456789"
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		s = s[:1+len(longestPrefix(s[1:], digits))]
	} else {
		s = longestPrefix(s, digits)
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func longestPrefix(s string, allowSet string) string {
sloop:
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(allowSet); j++ {
			if s[i] == allowSet[j] {
				continue sloop
			}
		}
		return s[:i]
	}
	return s
}

// Float returns the value as floating-point number
func (v Value) Float() float64 {
	if v.ptrOrType == 0 {
		return 0
	}
	if v.tls == nil {
		switch ColumnType(v.ptrOrType) {
		case TypeNull:
			return 0
		case TypeInteger:
			return float64(v.n)
		case TypeFloat:
			return v.float()
		case TypeBlob, TypeText:
			return castTextToReal(v.s)
		default:
			panic("unknown value type")
		}
	}
	return float64(lib.Xsqlite3_value_double(v.tls, v.ptrOrType))
}

func (v Value) float() float64 { return math.Float64frombits(uint64(v.n)) }

// castTextToReal emulates the SQLite CAST operator for a TEXT value to
// REAL, as documented in https://sqlite.org/lang_expr.html#castexpr
func castTextToReal(s string) float64 {
	s = strings.TrimSpace(s)
	for ; len(s) > 0; s = s[:len(s)-1] {
		n, err := strconv.ParseFloat(s, 64)
		if !errors.Is(err, strconv.ErrSyntax) {
			return n
		}
	}
	return 0
}

// Text returns the value as a string.
func (v Value) Text() string {
	if v.ptrOrType == 0 {
		return ""
	}
	if v.tls == nil {
		switch ColumnType(v.ptrOrType) {
		case TypeNull:
			return ""
		case TypeInteger:
			return strconv.FormatInt(v.n, 10)
		case TypeFloat:
			return strconv.FormatFloat(v.float(), 'g', -1, 64)
		case TypeText, TypeBlob:
			return v.s
		default:
			panic("unknown value type")
		}
	}
	ptr := lib.Xsqlite3_value_text(v.tls, v.ptrOrType)
	return goStringN(ptr, int(lib.Xsqlite3_value_bytes(v.tls, v.ptrOrType)))
}

// Blob returns a copy of the value as a blob.
func (v Value) Blob() []byte {
	if v.ptrOrType == 0 {
		return nil
	}
	if v.tls == nil {
		switch ColumnType(v.ptrOrType) {
		case TypeNull:
			return nil
		case TypeInteger:
			return strconv.AppendInt(nil, v.n, 10)
		case TypeFloat:
			return strconv.AppendFloat(nil, v.float(), 'g', -1, 64)
		case TypeBlob, TypeText:
			return []byte(v.s)
		default:
			panic("unknown value type")
		}
	}
	ptr := lib.Xsqlite3_value_blob(v.tls, v.ptrOrType)
	return libc.GoBytes(ptr, int(lib.Xsqlite3_value_bytes(v.tls, v.ptrOrType)))
}

// NoChange reports whether a column
// corresponding to this value in a [VTable] Update method
// is unchanged by the UPDATE operation
// that the Update method call was invoked to implement
// and if the prior [VTableCursor] Column method call that was invoked
// to extract the value for that column returned [Unchanged].
func (v Value) NoChange() bool {
	if v.ptrOrType == 0 {
		return v.n != 0
	}
	if v.tls == nil {
		return false
	}
	return lib.Xsqlite3_value_nochange(v.tls, v.ptrOrType) != 0
}

// FunctionImpl describes an [application-defined SQL function].
// Either Scalar or MakeAggregate must be set, but not both.
//
// [application-defined SQL function]: https://sqlite.org/appfunc.html
type FunctionImpl struct {
	// NArgs is the required number of arguments that the function accepts.
	// If NArgs is negative, then the function is variadic.
	//
	// Multiple function implementations may be registered with the same name
	// with different numbers of required arguments.
	NArgs int

	// Scalar is called when a scalar function is invoked in SQL.
	// The argument Values are not valid past the return of the function.
	Scalar func(ctx Context, args []Value) (Value, error)

	// MakeAggregate is called at the beginning of an evaluation of an aggregate function.
	MakeAggregate func(ctx Context) (AggregateFunction, error)

	// If Deterministic is true, the function must always give the same output
	// when the input parameters are the same. This enables functions to be used
	// in additional contexts like the WHERE clause of partial indexes and enables
	// additional optimizations.
	//
	// See https://sqlite.org/c3ref/c_deterministic.html#sqlitedeterministic for
	// more details.
	Deterministic bool

	// If AllowIndirect is false, then the function may only be invoked from
	// top-level SQL. If AllowIndirect is true, then the function can be used in
	// VIEWs, TRIGGERs, and schema structures (e.g. CHECK constraints and DEFAULT
	// clauses).
	//
	// This is the inverse of SQLITE_DIRECTONLY. See
	// https://sqlite.org/c3ref/c_deterministic.html#sqlitedirectonly for more
	// details. This defaults to false for better security.
	AllowIndirect bool
}

// An AggregateFunction is an invocation of an aggregate function.
// See the documentation for [aggregate function callbacks]
// and [application-defined window functions] for an overview.
//
// [aggregate function callbacks]: https://www.sqlite.org/appfunc.html#the_aggregate_function_callbacks
// [application-defined window functions]: https://www.sqlite.org/windowfunctions.html#user_defined_aggregate_window_functions
type AggregateFunction interface {
	// Step is called for each row
	// of an aggregate function's SQL invocation.
	// The argument Values are not valid past the return of the function.
	Step(ctx Context, rowArgs []Value) error

	// WindowInverse is called to remove
	// the oldest presently aggregated result of Step
	// from the current window.
	// The arguments are those passed to Step for the row being removed.
	// The argument Values are not valid past the return of the function.
	WindowInverse(ctx Context, rowArgs []Value) error

	// WindowValue is called to get the current value of an aggregate function.
	WindowValue(ctx Context) (Value, error)

	// Finalize is called after all of the aggregate function's input rows
	// have been stepped through.
	// No other methods will be called on the AggregateFunction after calling Finalize.
	Finalize(ctx Context)
}

// CreateFunction registers a Go function with SQLite
// for use in SQL queries.
//
// https://sqlite.org/appfunc.html
func (c *Conn) CreateFunction(name string, impl *FunctionImpl) error {
	if c == nil {
		return fmt.Errorf("sqlite: create function: nil connection")
	}
	if name == "" {
		return fmt.Errorf("sqlite: create function: no name provided")
	}
	if impl.NArgs > 127 {
		return fmt.Errorf("sqlite: create function %s: too many permitted arguments (%d)", name, impl.NArgs)
	}
	if impl.Scalar == nil && impl.MakeAggregate == nil {
		return fmt.Errorf("sqlite: create function %s: must specify one of Scalar or MakeAggregate", name)
	}
	if impl.Scalar != nil && impl.MakeAggregate != nil {
		return fmt.Errorf("sqlite: create function %s: both Scalar and MakeAggregate specified", name)
	}

	cname, err := libc.CString(name)
	if err != nil {
		return fmt.Errorf("sqlite: create function %s: %w", name, err)
	}
	defer libc.Xfree(c.tls, cname)

	eTextRep := int32(lib.SQLITE_UTF8)
	if impl.Deterministic {
		eTextRep |= lib.SQLITE_DETERMINISTIC
	}
	if !impl.AllowIndirect {
		eTextRep |= lib.SQLITE_DIRECTONLY
	}

	numArgs := impl.NArgs
	if numArgs < 0 {
		numArgs = -1
	}
	var res ResultCode
	if impl.Scalar != nil {
		xfuncs.mu.Lock()
		id := xfuncs.ids.next()
		xfuncs.m[id] = impl.Scalar
		xfuncs.mu.Unlock()

		res = ResultCode(lib.Xsqlite3_create_function_v2(
			c.tls,
			c.conn,
			cname,
			int32(numArgs),
			eTextRep,
			id,
			cFuncPointer(funcTrampoline),
			0,
			0,
			cFuncPointer(destroyScalarFunc),
		))
	} else {
		xAggregateFactories.mu.Lock()
		id := xAggregateFactories.ids.next()
		xAggregateFactories.m[id] = impl.MakeAggregate
		xAggregateFactories.mu.Unlock()

		res = ResultCode(lib.Xsqlite3_create_window_function(
			c.tls,
			c.conn,
			cname,
			int32(numArgs),
			eTextRep,
			id,
			cFuncPointer(stepTrampoline),
			cFuncPointer(finalTrampoline),
			cFuncPointer(valueTrampoline),
			cFuncPointer(inverseTrampoline),
			cFuncPointer(destroyAggregateFunc),
		))
	}
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: create function %s: %w", name, err)
	}
	return nil
}

var xfuncs = struct {
	mu  sync.RWMutex
	m   map[uintptr]func(Context, []Value) (Value, error)
	ids idGen
}{
	m: make(map[uintptr]func(Context, []Value) (Value, error)),
}

func funcTrampoline(tls *libc.TLS, ctx uintptr, n int32, valarray uintptr) {
	id := lib.Xsqlite3_user_data(tls, ctx)
	xfuncs.mu.RLock()
	x := xfuncs.m[id]
	xfuncs.mu.RUnlock()

	vals := make([]Value, 0, int(n))
	for ; len(vals) < cap(vals); valarray += uintptr(ptrSize) {
		vals = append(vals, Value{
			tls:       tls,
			ptrOrType: *(*uintptr)(unsafe.Pointer(valarray)),
		})
	}
	goCtx := Context{tls: tls, ptr: ctx}
	goCtx.result(x(goCtx, vals))
}

func destroyScalarFunc(tls *libc.TLS, id uintptr) {
	xfuncs.mu.Lock()
	defer xfuncs.mu.Unlock()
	delete(xfuncs.m, id)
	xfuncs.ids.reclaim(id)
}

var (
	xAggregateFactories = struct {
		mu  sync.RWMutex
		m   map[uintptr]func(Context) (AggregateFunction, error)
		ids idGen
	}{
		m: make(map[uintptr]func(Context) (AggregateFunction, error)),
	}

	xAggregateContext = struct {
		mu  sync.RWMutex
		m   map[uintptr]AggregateFunction
		ids idGen
	}{
		m: make(map[uintptr]AggregateFunction),
	}
)

func makeAggregate(tls *libc.TLS, ctx uintptr) (AggregateFunction, uintptr) {
	goCtx := Context{tls: tls, ptr: ctx}
	aggCtx := (*uintptr)(unsafe.Pointer(lib.Xsqlite3_aggregate_context(tls, ctx, int32(ptrSize))))
	if aggCtx == nil {
		goCtx.resultError(errors.New("insufficient memory for aggregate"))
		return nil, 0
	}
	if *aggCtx != 0 {
		// Already created.
		xAggregateContext.mu.RLock()
		f := xAggregateContext.m[*aggCtx]
		xAggregateContext.mu.RUnlock()
		return f, *aggCtx
	}

	factoryID := lib.Xsqlite3_user_data(tls, ctx)
	xAggregateFactories.mu.RLock()
	factory := xAggregateFactories.m[factoryID]
	xAggregateFactories.mu.RUnlock()

	f, err := factory(goCtx)
	if err != nil {
		goCtx.resultError(err)
		return nil, 0
	}
	if f == nil {
		goCtx.resultError(errors.New("MakeAggregate function returned nil"))
		return nil, 0
	}

	xAggregateContext.mu.Lock()
	*aggCtx = xAggregateContext.ids.next()
	xAggregateContext.m[*aggCtx] = f
	xAggregateContext.mu.Unlock()
	return f, *aggCtx
}

func stepTrampoline(tls *libc.TLS, ctx uintptr, n int32, valarray uintptr) {
	x, _ := makeAggregate(tls, ctx)
	if x == nil {
		return
	}

	vals := make([]Value, 0, int(n))
	for ; len(vals) < cap(vals); valarray += uintptr(ptrSize) {
		vals = append(vals, Value{
			tls:       tls,
			ptrOrType: *(*uintptr)(unsafe.Pointer(valarray)),
		})
	}
	goCtx := Context{tls: tls, ptr: ctx}
	if err := x.Step(goCtx, vals); err != nil {
		goCtx.resultError(err)
	}
}

func finalTrampoline(tls *libc.TLS, ctx uintptr) {
	x, id := makeAggregate(tls, ctx)
	if x == nil {
		return
	}
	goCtx := Context{tls: tls, ptr: ctx}
	goCtx.result(x.WindowValue(goCtx))
	x.Finalize(goCtx)

	xAggregateContext.mu.Lock()
	defer xAggregateContext.mu.Unlock()
	delete(xAggregateContext.m, id)
	xAggregateContext.ids.reclaim(id)
}

func valueTrampoline(tls *libc.TLS, ctx uintptr) {
	x, _ := makeAggregate(tls, ctx)
	if x == nil {
		return
	}

	goCtx := Context{tls: tls, ptr: ctx}
	goCtx.result(x.WindowValue(goCtx))
}

func inverseTrampoline(tls *libc.TLS, ctx uintptr, n int32, valarray uintptr) {
	x, _ := makeAggregate(tls, ctx)
	if x == nil {
		return
	}

	vals := make([]Value, 0, int(n))
	for ; len(vals) < cap(vals); valarray += uintptr(ptrSize) {
		vals = append(vals, Value{
			tls:       tls,
			ptrOrType: *(*uintptr)(unsafe.Pointer(valarray)),
		})
	}
	goCtx := Context{tls: tls, ptr: ctx}
	if err := x.WindowInverse(goCtx, vals); err != nil {
		goCtx.resultError(err)
	}
}

func destroyAggregateFunc(tls *libc.TLS, id uintptr) {
	xAggregateFactories.mu.Lock()
	defer xAggregateFactories.mu.Unlock()
	delete(xAggregateFactories.m, id)
	xAggregateFactories.ids.reclaim(id)
}

// CollatingFunc is a [collating function/sequence],
// that is, a function that compares two strings.
// The function returns a negative number if a < b,
// a positive number if a > b,
// or zero if a == b.
// A collating function must always return the same answer given the same inputs.
// The collating function must obey the following properties for all strings A, B, and C:
//
//  1. If A==B then B==A.
//  2. If A==B and B==C then A==C.
//  3. If A<B then B>A.
//  4. If A<B and B<C then A<C.
//
// [collating function/sequence]: https://www.sqlite.org/datatype3.html#collation
type CollatingFunc func(a, b string) int

// SetCollation sets the [collating function] for the given name.
//
// [collating function]: https://www.sqlite.org/datatype3.html#collation
func (c *Conn) SetCollation(name string, compare CollatingFunc) error {
	verb := "create"
	if compare == nil {
		verb = "remove"
	}

	if c == nil {
		return fmt.Errorf("sqlite: %s collation: nil connection", verb)
	}
	if name == "" {
		return fmt.Errorf("sqlite: %s collation: no name provided", verb)
	}

	cname, err := libc.CString(name)
	if err != nil {
		return fmt.Errorf("sqlite: %s collation: no name provided", verb)
	}
	defer libc.Xfree(c.tls, cname)

	if compare == nil {
		res := ResultCode(lib.Xsqlite3_create_collation_v2(
			c.tls, c.conn, cname, lib.SQLITE_UTF8, 0, 0, 0,
		))
		if err := res.ToError(); err != nil {
			return fmt.Errorf("sqlite: %s collation %s: %w", verb, name, err)
		}
		return nil
	}

	xcollations.mu.Lock()
	id := xcollations.ids.next()
	xcollations.m[id] = compare
	xcollations.mu.Unlock()

	res := ResultCode(lib.Xsqlite3_create_collation_v2(
		c.tls, c.conn, cname, lib.SQLITE_UTF8, id, cFuncPointer(collationTrampoline), cFuncPointer(destroyCollation),
	))
	if err := res.ToError(); err != nil {
		destroyCollation(c.tls, id)
		return fmt.Errorf("sqlite: %s collation %s: %w", verb, name, err)
	}
	return nil
}

var xcollations = struct {
	mu  sync.RWMutex
	m   map[uintptr]CollatingFunc
	ids idGen
}{
	m: make(map[uintptr]CollatingFunc),
}

func collationTrampoline(tls *libc.TLS, id uintptr, n1 int32, p1 uintptr, n2 int32, p2 uintptr) int32 {
	xcollations.mu.RLock()
	f := xcollations.m[id]
	xcollations.mu.RUnlock()

	s1 := goStringN(p1, int(n1))
	s2 := goStringN(p2, int(n2))
	switch x := f(s1, s2); {
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return 0
	}
}

func destroyCollation(tls *libc.TLS, id uintptr) {
	xcollations.mu.Lock()
	defer xcollations.mu.Unlock()
	delete(xcollations.m, id)
	xcollations.ids.reclaim(id)
}

// idGen is an ID generator. The zero value is ready to use.
type idGen struct {
	bitset []uint64
}

func (gen *idGen) next() uintptr {
	base := uintptr(1)
	for i := 0; i < len(gen.bitset); i, base = i+1, base+64 {
		b := gen.bitset[i]
		if b != 1<<64-1 {
			n := uintptr(bits.TrailingZeros64(^b))
			gen.bitset[i] |= 1 << n
			return base + n
		}
	}
	gen.bitset = append(gen.bitset, 1)
	return base
}

func (gen *idGen) reclaim(id uintptr) {
	bit := id - 1
	gen.bitset[bit/64] &^= 1 << (bit % 64)
}
