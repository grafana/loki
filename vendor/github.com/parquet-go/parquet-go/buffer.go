package parquet

import (
	"log"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/parquet-go/parquet-go/internal/debug"
)

// GenericBuffer is similar to a Buffer but uses a type parameter to define the
// Go type representing the schema of rows in the buffer.
//
// See GenericWriter for details about the benefits over the classic Buffer API.
type GenericBuffer[T any] struct {
	base  Buffer
	write bufferFunc[T]
}

// NewGenericBuffer is like NewBuffer but returns a GenericBuffer[T] suited to write
// rows of Go type T.
//
// The type parameter T should be a map, struct, or any. Any other types will
// cause a panic at runtime. Type checking is a lot more effective when the
// generic parameter is a struct type, using map and interface types is somewhat
// similar to using a Writer.  If using an interface type for the type parameter,
// then providing a schema at instantiation is required.
//
// If the option list may explicitly declare a schema, it must be compatible
// with the schema generated from T.
func NewGenericBuffer[T any](options ...RowGroupOption) *GenericBuffer[T] {
	config, err := NewRowGroupConfig(options...)
	if err != nil {
		panic(err)
	}

	t := typeOf[T]()
	if config.Schema == nil && t != nil {
		config.Schema = schemaOf(dereference(t))
	}

	if config.Schema == nil {
		panic("generic buffer must be instantiated with schema or concrete type.")
	}

	buf := &GenericBuffer[T]{
		base: Buffer{config: config},
	}
	buf.base.configure(config.Schema)
	buf.write = bufferFuncOf[T](t, config.Schema)
	return buf
}

func typeOf[T any]() reflect.Type {
	var v T
	return reflect.TypeOf(v)
}

type bufferFunc[T any] func(*GenericBuffer[T], []T) (int, error)

func bufferFuncOf[T any](t reflect.Type, schema *Schema) bufferFunc[T] {
	if t == nil {
		return (*GenericBuffer[T]).writeRows
	}
	switch t.Kind() {
	case reflect.Interface, reflect.Map:
		return (*GenericBuffer[T]).writeRows

	case reflect.Struct:
		return makeBufferFunc[T](t, schema)

	case reflect.Pointer:
		if e := t.Elem(); e.Kind() == reflect.Struct {
			return makeBufferFunc[T](t, schema)
		}
	}
	panic("cannot create buffer for values of type " + t.String())
}

func makeBufferFunc[T any](t reflect.Type, schema *Schema) bufferFunc[T] {
	writeRows := writeRowsFuncOf(t, schema, nil)
	return func(buf *GenericBuffer[T], rows []T) (n int, err error) {
		err = writeRows(buf.base.columns, makeArrayOf(rows), columnLevels{})
		if err == nil {
			n = len(rows)
		}
		return n, err
	}
}

func (buf *GenericBuffer[T]) Size() int64 {
	return buf.base.Size()
}

func (buf *GenericBuffer[T]) NumRows() int64 {
	return buf.base.NumRows()
}

func (buf *GenericBuffer[T]) ColumnChunks() []ColumnChunk {
	return buf.base.ColumnChunks()
}

func (buf *GenericBuffer[T]) ColumnBuffers() []ColumnBuffer {
	return buf.base.ColumnBuffers()
}

func (buf *GenericBuffer[T]) SortingColumns() []SortingColumn {
	return buf.base.SortingColumns()
}

func (buf *GenericBuffer[T]) Len() int {
	return buf.base.Len()
}

func (buf *GenericBuffer[T]) Less(i, j int) bool {
	return buf.base.Less(i, j)
}

func (buf *GenericBuffer[T]) Swap(i, j int) {
	buf.base.Swap(i, j)
}

func (buf *GenericBuffer[T]) Reset() {
	buf.base.Reset()
}

func (buf *GenericBuffer[T]) Write(rows []T) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	return buf.write(buf, rows)
}

func (buf *GenericBuffer[T]) WriteRows(rows []Row) (int, error) {
	return buf.base.WriteRows(rows)
}

func (buf *GenericBuffer[T]) WriteRowGroup(rowGroup RowGroup) (int64, error) {
	return buf.base.WriteRowGroup(rowGroup)
}

func (buf *GenericBuffer[T]) Rows() Rows {
	return buf.base.Rows()
}

func (buf *GenericBuffer[T]) Schema() *Schema {
	return buf.base.Schema()
}

func (buf *GenericBuffer[T]) writeRows(rows []T) (int, error) {
	if cap(buf.base.rowbuf) < len(rows) {
		buf.base.rowbuf = make([]Row, len(rows))
	} else {
		buf.base.rowbuf = buf.base.rowbuf[:len(rows)]
	}
	defer clearRows(buf.base.rowbuf)

	schema := buf.base.Schema()
	for i := range rows {
		buf.base.rowbuf[i] = schema.Deconstruct(buf.base.rowbuf[i], &rows[i])
	}

	return buf.base.WriteRows(buf.base.rowbuf)
}

var (
	_ RowGroup       = (*GenericBuffer[any])(nil)
	_ RowGroupWriter = (*GenericBuffer[any])(nil)
	_ sort.Interface = (*GenericBuffer[any])(nil)

	_ RowGroup       = (*GenericBuffer[struct{}])(nil)
	_ RowGroupWriter = (*GenericBuffer[struct{}])(nil)
	_ sort.Interface = (*GenericBuffer[struct{}])(nil)

	_ RowGroup       = (*GenericBuffer[map[struct{}]struct{}])(nil)
	_ RowGroupWriter = (*GenericBuffer[map[struct{}]struct{}])(nil)
	_ sort.Interface = (*GenericBuffer[map[struct{}]struct{}])(nil)
)

// Buffer represents an in-memory group of parquet rows.
//
// The main purpose of the Buffer type is to provide a way to sort rows before
// writing them to a parquet file. Buffer implements sort.Interface as a way
// to support reordering the rows that have been written to it.
type Buffer struct {
	config  *RowGroupConfig
	schema  *Schema
	rowbuf  []Row
	colbuf  [][]Value
	chunks  []ColumnChunk
	columns []ColumnBuffer
	sorted  []ColumnBuffer
}

// NewBuffer constructs a new buffer, using the given list of buffer options
// to configure the buffer returned by the function.
//
// The function panics if the buffer configuration is invalid. Programs that
// cannot guarantee the validity of the options passed to NewBuffer should
// construct the buffer configuration independently prior to calling this
// function:
//
//	config, err := parquet.NewRowGroupConfig(options...)
//	if err != nil {
//		// handle the configuration error
//		...
//	} else {
//		// this call to create a buffer is guaranteed not to panic
//		buffer := parquet.NewBuffer(config)
//		...
//	}
func NewBuffer(options ...RowGroupOption) *Buffer {
	config, err := NewRowGroupConfig(options...)
	if err != nil {
		panic(err)
	}
	buf := &Buffer{
		config: config,
	}
	if config.Schema != nil {
		buf.configure(config.Schema)
	}
	return buf
}

func (buf *Buffer) configure(schema *Schema) {
	if schema == nil {
		return
	}
	sortingColumns := buf.config.Sorting.SortingColumns
	buf.sorted = make([]ColumnBuffer, len(sortingColumns))

	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		nullOrdering := nullsGoLast
		columnIndex := int(leaf.columnIndex)
		columnType := leaf.node.Type()
		bufferCap := buf.config.ColumnBufferCapacity
		dictionary := (Dictionary)(nil)
		encoding := encodingOf(leaf.node)

		if isDictionaryEncoding(encoding) {
			estimatedDictBufferSize := columnType.EstimateSize(bufferCap)
			dictBuffer := columnType.NewValues(
				make([]byte, 0, estimatedDictBufferSize),
				nil,
			)
			dictionary = columnType.NewDictionary(columnIndex, 0, dictBuffer)
			columnType = dictionary.Type()
		}

		sortingIndex := searchSortingColumn(sortingColumns, leaf.path)
		if sortingIndex < len(sortingColumns) && sortingColumns[sortingIndex].NullsFirst() {
			nullOrdering = nullsGoFirst
		}

		column := columnType.NewColumnBuffer(columnIndex, bufferCap)
		switch {
		case leaf.maxRepetitionLevel > 0:
			column = newRepeatedColumnBuffer(column, leaf.maxRepetitionLevel, leaf.maxDefinitionLevel, nullOrdering)
		case leaf.maxDefinitionLevel > 0:
			column = newOptionalColumnBuffer(column, leaf.maxDefinitionLevel, nullOrdering)
		}
		buf.columns = append(buf.columns, column)

		if sortingIndex < len(sortingColumns) {
			if sortingColumns[sortingIndex].Descending() {
				column = &reversedColumnBuffer{column}
			}
			buf.sorted[sortingIndex] = column
		}
	})

	buf.schema = schema
	buf.rowbuf = make([]Row, 0, 1)
	buf.colbuf = make([][]Value, len(buf.columns))
	buf.chunks = make([]ColumnChunk, len(buf.columns))

	for i, column := range buf.columns {
		buf.chunks[i] = column
	}
}

// Size returns the estimated size of the buffer in memory (in bytes).
func (buf *Buffer) Size() int64 {
	size := int64(0)
	for _, col := range buf.columns {
		size += col.Size()
	}
	return size
}

// NumRows returns the number of rows written to the buffer.
func (buf *Buffer) NumRows() int64 { return int64(buf.Len()) }

// ColumnChunks returns the buffer columns.
func (buf *Buffer) ColumnChunks() []ColumnChunk { return buf.chunks }

// ColumnBuffer returns the buffer columns.
//
// This method is similar to ColumnChunks, but returns a list of ColumnBuffer
// instead of a ColumnChunk values (the latter being read-only); calling
// ColumnBuffers or ColumnChunks with the same index returns the same underlying
// objects, but with different types, which removes the need for making a type
// assertion if the program needed to write directly to the column buffers.
// The presence of the ColumnChunks method is still required to satisfy the
// RowGroup interface.
func (buf *Buffer) ColumnBuffers() []ColumnBuffer { return buf.columns }

// Schema returns the schema of the buffer.
//
// The schema is either configured by passing a Schema in the option list when
// constructing the buffer, or lazily discovered when the first row is written.
func (buf *Buffer) Schema() *Schema { return buf.schema }

// SortingColumns returns the list of columns by which the buffer will be
// sorted.
//
// The sorting order is configured by passing a SortingColumns option when
// constructing the buffer.
func (buf *Buffer) SortingColumns() []SortingColumn { return buf.config.Sorting.SortingColumns }

// Len returns the number of rows written to the buffer.
func (buf *Buffer) Len() int {
	if len(buf.columns) == 0 {
		return 0
	} else {
		// All columns have the same number of rows.
		return buf.columns[0].Len()
	}
}

// Less returns true if row[i] < row[j] in the buffer.
func (buf *Buffer) Less(i, j int) bool {
	for _, col := range buf.sorted {
		switch {
		case col.Less(i, j):
			return true
		case col.Less(j, i):
			return false
		}
	}
	return false
}

// Swap exchanges the rows at indexes i and j.
func (buf *Buffer) Swap(i, j int) {
	for _, col := range buf.columns {
		col.Swap(i, j)
	}
}

// Reset clears the content of the buffer, allowing it to be reused.
func (buf *Buffer) Reset() {
	for _, col := range buf.columns {
		col.Reset()
	}
}

// Write writes a row held in a Go value to the buffer.
func (buf *Buffer) Write(row interface{}) error {
	if buf.schema == nil {
		buf.configure(SchemaOf(row))
	}

	buf.rowbuf = buf.rowbuf[:1]
	defer clearRows(buf.rowbuf)

	buf.rowbuf[0] = buf.schema.Deconstruct(buf.rowbuf[0], row)
	_, err := buf.WriteRows(buf.rowbuf)
	return err
}

// WriteRows writes parquet rows to the buffer.
func (buf *Buffer) WriteRows(rows []Row) (int, error) {
	defer func() {
		for i, colbuf := range buf.colbuf {
			clearValues(colbuf)
			buf.colbuf[i] = colbuf[:0]
		}
	}()

	if buf.schema == nil {
		return 0, ErrRowGroupSchemaMissing
	}

	for _, row := range rows {
		for _, value := range row {
			columnIndex := value.Column()
			buf.colbuf[columnIndex] = append(buf.colbuf[columnIndex], value)
		}
	}

	for columnIndex, values := range buf.colbuf {
		if _, err := buf.columns[columnIndex].WriteValues(values); err != nil {
			// TODO: an error at this stage will leave the buffer in an invalid
			// state since the row was partially written. Applications are not
			// expected to continue using the buffer after getting an error,
			// maybe we can enforce it?
			return 0, err
		}
	}

	return len(rows), nil
}

// WriteRowGroup satisfies the RowGroupWriter interface.
func (buf *Buffer) WriteRowGroup(rowGroup RowGroup) (int64, error) {
	rowGroupSchema := rowGroup.Schema()
	switch {
	case rowGroupSchema == nil:
		return 0, ErrRowGroupSchemaMissing
	case buf.schema == nil:
		buf.configure(rowGroupSchema)
	case !nodesAreEqual(buf.schema, rowGroupSchema):
		return 0, ErrRowGroupSchemaMismatch
	}
	if !sortingColumnsHavePrefix(rowGroup.SortingColumns(), buf.SortingColumns()) {
		return 0, ErrRowGroupSortingColumnsMismatch
	}
	n := buf.NumRows()
	r := rowGroup.Rows()
	defer r.Close()
	_, err := CopyRows(bufferWriter{buf}, r)
	return buf.NumRows() - n, err
}

// Rows returns a reader exposing the current content of the buffer.
//
// The buffer and the returned reader share memory. Mutating the buffer
// concurrently to reading rows may result in non-deterministic behavior.
func (buf *Buffer) Rows() Rows { return newRowGroupRows(buf, ReadModeSync) }

// bufferWriter is an adapter for Buffer which implements both RowWriter and
// PageWriter to enable optimizations in CopyRows for types that support writing
// rows by copying whole pages instead of calling WriteRow repeatedly.
type bufferWriter struct{ buf *Buffer }

func (w bufferWriter) WriteRows(rows []Row) (int, error) {
	return w.buf.WriteRows(rows)
}

func (w bufferWriter) WriteValues(values []Value) (int, error) {
	return w.buf.columns[values[0].Column()].WriteValues(values)
}

func (w bufferWriter) WritePage(page Page) (int64, error) {
	return CopyValues(w.buf.columns[page.Column()], page.Values())
}

var (
	_ RowGroup       = (*Buffer)(nil)
	_ RowGroupWriter = (*Buffer)(nil)
	_ sort.Interface = (*Buffer)(nil)

	_ RowWriter   = (*bufferWriter)(nil)
	_ PageWriter  = (*bufferWriter)(nil)
	_ ValueWriter = (*bufferWriter)(nil)
)

type buffer struct {
	data  []byte
	refc  uintptr
	pool  *bufferPool
	stack []byte
}

func (b *buffer) refCount() int {
	return int(atomic.LoadUintptr(&b.refc))
}

func (b *buffer) ref() {
	atomic.AddUintptr(&b.refc, +1)
}

func (b *buffer) unref() {
	if atomic.AddUintptr(&b.refc, ^uintptr(0)) == 0 {
		if b.pool != nil {
			b.pool.put(b)
		}
	}
}

func monitorBufferRelease(b *buffer) {
	if rc := b.refCount(); rc != 0 {
		log.Printf("PARQUETGODEBUG: buffer garbage collected with non-zero reference count\n%s", string(b.stack))
	}
}

type bufferPool struct {
	// Buckets are split in two groups for short and large buffers. In the short
	// buffer group (below 256KB), the growth rate between each bucket is 2. The
	// growth rate changes to 1.5 in the larger buffer group.
	//
	// Short buffer buckets:
	// ---------------------
	//   4K, 8K, 16K, 32K, 64K, 128K, 256K
	//
	// Large buffer buckets:
	// ---------------------
	//   364K, 546K, 819K ...
	//
	buckets [bufferPoolBucketCount]sync.Pool
}

func (p *bufferPool) newBuffer(bufferSize, bucketSize int) *buffer {
	b := &buffer{
		data: make([]byte, bufferSize, bucketSize),
		refc: 1,
		pool: p,
	}
	if debug.TRACEBUF > 0 {
		b.stack = make([]byte, 4096)
		runtime.SetFinalizer(b, monitorBufferRelease)
	}
	return b
}

// get returns a buffer from the levelled buffer pool. size is used to choose
// the appropriate pool.
func (p *bufferPool) get(bufferSize int) *buffer {
	bucketIndex, bucketSize := bufferPoolBucketIndexAndSizeOfGet(bufferSize)

	b := (*buffer)(nil)
	if bucketIndex >= 0 {
		b, _ = p.buckets[bucketIndex].Get().(*buffer)
	}

	if b == nil {
		b = p.newBuffer(bufferSize, bucketSize)
	} else {
		b.data = b.data[:bufferSize]
		b.ref()
	}

	if debug.TRACEBUF > 0 {
		b.stack = b.stack[:runtime.Stack(b.stack[:cap(b.stack)], false)]
	}
	return b
}

func (p *bufferPool) put(b *buffer) {
	if b.pool != p {
		panic("BUG: buffer returned to a different pool than the one it was allocated from")
	}
	if b.refCount() != 0 {
		panic("BUG: buffer returned to pool with a non-zero reference count")
	}
	if bucketIndex, _ := bufferPoolBucketIndexAndSizeOfPut(cap(b.data)); bucketIndex >= 0 {
		p.buckets[bucketIndex].Put(b)
	}
}

const (
	bufferPoolBucketCount         = 32
	bufferPoolMinSize             = 4096
	bufferPoolLastShortBucketSize = 262144
)

func bufferPoolNextSize(size int) int {
	if size < bufferPoolLastShortBucketSize {
		return size * 2
	} else {
		return size + (size / 2)
	}
}

func bufferPoolBucketIndexAndSizeOfGet(size int) (int, int) {
	limit := bufferPoolMinSize

	for i := 0; i < bufferPoolBucketCount; i++ {
		if size <= limit {
			return i, limit
		}
		limit = bufferPoolNextSize(limit)
	}

	return -1, size
}

func bufferPoolBucketIndexAndSizeOfPut(size int) (int, int) {
	// When releasing buffers, some may have a capacity that is not one of the
	// bucket sizes (due to the use of append for example). In this case, we
	// have to put the buffer is the highest bucket with a size less or equal
	// to the buffer capacity.
	if limit := bufferPoolMinSize; size >= limit {
		for i := 0; i < bufferPoolBucketCount; i++ {
			n := bufferPoolNextSize(limit)
			if size < n {
				return i, limit
			}
			limit = n
		}
	}
	return -1, size
}

var (
	buffers bufferPool
)

type bufferedPage struct {
	Page
	values           *buffer
	offsets          *buffer
	repetitionLevels *buffer
	definitionLevels *buffer
}

func newBufferedPage(page Page, values, offsets, definitionLevels, repetitionLevels *buffer) *bufferedPage {
	p := &bufferedPage{
		Page:             page,
		values:           values,
		offsets:          offsets,
		definitionLevels: definitionLevels,
		repetitionLevels: repetitionLevels,
	}
	bufferRef(values)
	bufferRef(offsets)
	bufferRef(definitionLevels)
	bufferRef(repetitionLevels)
	return p
}

func (p *bufferedPage) Slice(i, j int64) Page {
	return newBufferedPage(
		p.Page.Slice(i, j),
		p.values,
		p.offsets,
		p.definitionLevels,
		p.repetitionLevels,
	)
}

func (p *bufferedPage) Retain() {
	bufferRef(p.values)
	bufferRef(p.offsets)
	bufferRef(p.definitionLevels)
	bufferRef(p.repetitionLevels)
}

func (p *bufferedPage) Release() {
	bufferUnref(p.values)
	bufferUnref(p.offsets)
	bufferUnref(p.definitionLevels)
	bufferUnref(p.repetitionLevels)
}

func bufferRef(buf *buffer) {
	if buf != nil {
		buf.ref()
	}
}

func bufferUnref(buf *buffer) {
	if buf != nil {
		buf.unref()
	}
}

// Retain is a helper function to increment the reference counter of pages
// backed by memory which can be granularly managed by the application.
//
// Usage of this function is optional and with Release, is intended to allow
// finer grain memory management in the application. Most programs should be
// able to rely on automated memory management provided by the Go garbage
// collector instead.
//
// The function should be called when a page lifetime is about to be shared
// between multiple goroutines or layers of an application, and the program
// wants to express "sharing ownership" of the page.
//
// Calling this function on pages that do not embed a reference counter does
// nothing.
func Retain(page Page) {
	if p, _ := page.(retainable); p != nil {
		p.Retain()
	}
}

// Release is a helper function to decrement the reference counter of pages
// backed by memory which can be granularly managed by the application.
//
// Usage of this is optional and with Retain, is intended to allow finer grained
// memory management in the application, at the expense of potentially causing
// panics if the page is used after its reference count has reached zero. Most
// programs should be able to rely on automated memory management provided by
// the Go garbage collector instead.
//
// The function should be called to return a page to the internal buffer pool,
// when a goroutine "releases ownership" it acquired either by being the single
// owner (e.g. capturing the return value from a ReadPage call) or having gotten
// shared ownership by calling Retain.
//
// Calling this function on pages that do not embed a reference counter does
// nothing.
func Release(page Page) {
	if p, _ := page.(releasable); p != nil {
		p.Release()
	}
}

type retainable interface {
	Retain()
}

type releasable interface {
	Release()
}

var (
	_ retainable = (*bufferedPage)(nil)
	_ releasable = (*bufferedPage)(nil)
)
