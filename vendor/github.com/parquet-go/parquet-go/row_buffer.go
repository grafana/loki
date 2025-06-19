package parquet

import (
	"io"
	"sort"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
)

// RowBuffer is an implementation of the RowGroup interface which stores parquet
// rows in memory.
//
// Unlike GenericBuffer which uses a column layout to store values in memory
// buffers, RowBuffer uses a row layout. The use of row layout provides greater
// efficiency when sorting the buffer, which is the primary use case for the
// RowBuffer type. Applications which intend to sort rows prior to writing them
// to a parquet file will often see lower CPU utilization from using a RowBuffer
// than a GenericBuffer.
//
// RowBuffer values are not safe to use concurrently from multiple goroutines.
type RowBuffer[T any] struct {
	alloc   rowAllocator
	schema  *Schema
	sorting []SortingColumn
	rows    []Row
	values  []Value
	compare func(Row, Row) int
}

// NewRowBuffer constructs a new row buffer.
func NewRowBuffer[T any](options ...RowGroupOption) *RowBuffer[T] {
	config := DefaultRowGroupConfig()
	config.Apply(options...)
	if err := config.Validate(); err != nil {
		panic(err)
	}

	t := typeOf[T]()
	if config.Schema == nil && t != nil {
		config.Schema = schemaOf(dereference(t))
	}

	if config.Schema == nil {
		panic("row buffer must be instantiated with schema or concrete type.")
	}

	return &RowBuffer[T]{
		schema:  config.Schema,
		sorting: config.Sorting.SortingColumns,
		compare: config.Schema.Comparator(config.Sorting.SortingColumns...),
	}
}

// Reset clears the content of the buffer without releasing its memory.
func (buf *RowBuffer[T]) Reset() {
	for i := range buf.rows {
		buf.rows[i] = nil
	}
	for i := range buf.values {
		buf.values[i] = Value{}
	}
	buf.rows = buf.rows[:0]
	buf.values = buf.values[:0]
	buf.alloc.reset()
}

// NumRows returns the number of rows currently written to the buffer.
func (buf *RowBuffer[T]) NumRows() int64 { return int64(len(buf.rows)) }

// ColumnChunks returns a view of the buffer's columns.
//
// Note that reading columns of a RowBuffer will be less efficient than reading
// columns of a GenericBuffer since the latter uses a column layout. This method
// is mainly exposed to satisfy the RowGroup interface, applications which need
// compute-efficient column scans on in-memory buffers should likely use a
// GenericBuffer instead.
//
// The returned column chunks are snapshots at the time the method is called,
// they remain valid until the next call to Reset on the buffer.
func (buf *RowBuffer[T]) ColumnChunks() []ColumnChunk {
	columns := buf.schema.Columns()
	chunks := make([]rowBufferColumnChunk, len(columns))

	for i, column := range columns {
		leafColumn, _ := buf.schema.Lookup(column...)
		chunks[i] = rowBufferColumnChunk{
			page: rowBufferPage{
				rows:               buf.rows,
				typ:                leafColumn.Node.Type(),
				column:             leafColumn.ColumnIndex,
				maxRepetitionLevel: byte(leafColumn.MaxRepetitionLevel),
				maxDefinitionLevel: byte(leafColumn.MaxDefinitionLevel),
			},
		}
	}

	columnChunks := make([]ColumnChunk, len(chunks))
	for i := range chunks {
		columnChunks[i] = &chunks[i]
	}
	return columnChunks
}

// SortingColumns returns the list of columns that rows are expected to be
// sorted by.
//
// The list of sorting columns is configured when the buffer is created and used
// when it is sorted.
//
// Note that unless the buffer is explicitly sorted, there are no guarantees
// that the rows it contains will be in the order specified by the sorting
// columns.
func (buf *RowBuffer[T]) SortingColumns() []SortingColumn { return buf.sorting }

// Schema returns the schema of rows in the buffer.
func (buf *RowBuffer[T]) Schema() *Schema { return buf.schema }

// Len returns the number of rows in the buffer.
//
// The method contributes to satisfying sort.Interface.
func (buf *RowBuffer[T]) Len() int { return len(buf.rows) }

// Less compares the rows at index i and j according to the sorting columns
// configured on the buffer.
//
// The method contributes to satisfying sort.Interface.
func (buf *RowBuffer[T]) Less(i, j int) bool {
	return buf.compare(buf.rows[i], buf.rows[j]) < 0
}

// Swap exchanges the rows at index i and j in the buffer.
//
// The method contributes to satisfying sort.Interface.
func (buf *RowBuffer[T]) Swap(i, j int) {
	buf.rows[i], buf.rows[j] = buf.rows[j], buf.rows[i]
}

// Rows returns a Rows instance exposing rows stored in the buffer.
//
// The rows returned are a snapshot at the time the method is called.
// The returned rows and values read from it remain valid until the next call
// to Reset on the buffer.
func (buf *RowBuffer[T]) Rows() Rows {
	return &rowBufferRows{rows: buf.rows, schema: buf.schema}
}

// Write writes rows to the buffer, returning the number of rows written.
func (buf *RowBuffer[T]) Write(rows []T) (int, error) {
	for i := range rows {
		off := len(buf.values)
		buf.values = buf.schema.Deconstruct(buf.values, &rows[i])
		end := len(buf.values)
		row := buf.values[off:end:end]
		buf.alloc.capture(row)
		buf.rows = append(buf.rows, row)
	}
	return len(rows), nil
}

// WriteRows writes parquet rows to the buffer, returing the number of rows
// written.
func (buf *RowBuffer[T]) WriteRows(rows []Row) (int, error) {
	for i := range rows {
		off := len(buf.values)
		buf.values = append(buf.values, rows[i]...)
		end := len(buf.values)
		row := buf.values[off:end:end]
		buf.alloc.capture(row)
		buf.rows = append(buf.rows, row)
	}
	return len(rows), nil
}

type rowBufferColumnChunk struct{ page rowBufferPage }

func (c *rowBufferColumnChunk) Type() Type { return c.page.Type() }

func (c *rowBufferColumnChunk) Column() int { return c.page.Column() }

func (c *rowBufferColumnChunk) Pages() Pages { return onePage(&c.page) }

func (c *rowBufferColumnChunk) ColumnIndex() (ColumnIndex, error) { return nil, nil }

func (c *rowBufferColumnChunk) OffsetIndex() (OffsetIndex, error) { return nil, nil }

func (c *rowBufferColumnChunk) BloomFilter() BloomFilter { return nil }

func (c *rowBufferColumnChunk) NumValues() int64 { return c.page.NumValues() }

type rowBufferPage struct {
	rows               []Row
	typ                Type
	column             int
	maxRepetitionLevel byte
	maxDefinitionLevel byte
}

func (p *rowBufferPage) Type() Type { return p.typ }

func (p *rowBufferPage) Column() int { return p.column }

func (p *rowBufferPage) Dictionary() Dictionary { return nil }

func (p *rowBufferPage) NumRows() int64 { return int64(len(p.rows)) }

func (p *rowBufferPage) NumValues() int64 {
	numValues := int64(0)
	p.scan(func(value Value) {
		if !value.isNull() {
			numValues++
		}
	})
	return numValues
}

func (p *rowBufferPage) NumNulls() int64 {
	numNulls := int64(0)
	p.scan(func(value Value) {
		if value.isNull() {
			numNulls++
		}
	})
	return numNulls
}

func (p *rowBufferPage) Bounds() (min, max Value, ok bool) {
	p.scan(func(value Value) {
		if !value.IsNull() {
			switch {
			case !ok:
				min, max, ok = value, value, true
			case p.typ.Compare(value, min) < 0:
				min = value
			case p.typ.Compare(value, max) > 0:
				max = value
			}
		}
	})
	return min, max, ok
}

func (p *rowBufferPage) Size() int64 { return 0 }

func (p *rowBufferPage) Values() ValueReader {
	return &rowBufferPageValueReader{
		page:        p,
		columnIndex: ^int16(p.column),
	}
}

func (p *rowBufferPage) Clone() Page {
	rows := make([]Row, len(p.rows))
	for i := range rows {
		rows[i] = p.rows[i].Clone()
	}
	return &rowBufferPage{
		rows:   rows,
		typ:    p.typ,
		column: p.column,
	}
}

func (p *rowBufferPage) Slice(i, j int64) Page {
	return &rowBufferPage{
		rows:   p.rows[i:j],
		typ:    p.typ,
		column: p.column,
	}
}

func (p *rowBufferPage) RepetitionLevels() (repetitionLevels []byte) {
	if p.maxRepetitionLevel != 0 {
		repetitionLevels = make([]byte, 0, len(p.rows))
		p.scan(func(value Value) {
			repetitionLevels = append(repetitionLevels, value.repetitionLevel)
		})
	}
	return repetitionLevels
}

func (p *rowBufferPage) DefinitionLevels() (definitionLevels []byte) {
	if p.maxDefinitionLevel != 0 {
		definitionLevels = make([]byte, 0, len(p.rows))
		p.scan(func(value Value) {
			definitionLevels = append(definitionLevels, value.definitionLevel)
		})
	}
	return definitionLevels
}

func (p *rowBufferPage) Data() encoding.Values {
	switch p.typ.Kind() {
	case Boolean:
		values := make([]byte, (len(p.rows)+7)/8)
		numValues := 0
		p.scanNonNull(func(value Value) {
			if value.boolean() {
				i := uint(numValues) / 8
				j := uint(numValues) % 8
				values[i] |= 1 << j
			}
			numValues++
		})
		return encoding.BooleanValues(values[:(numValues+7)/8])

	case Int32:
		values := make([]int32, 0, len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.int32()) })
		return encoding.Int32Values(values)

	case Int64:
		values := make([]int64, 0, len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.int64()) })
		return encoding.Int64Values(values)

	case Int96:
		values := make([]deprecated.Int96, 0, len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.int96()) })
		return encoding.Int96Values(values)

	case Float:
		values := make([]float32, 0, len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.float()) })
		return encoding.FloatValues(values)

	case Double:
		values := make([]float64, 0, len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.double()) })
		return encoding.DoubleValues(values)

	case ByteArray:
		values := make([]byte, 0, p.typ.EstimateSize(len(p.rows)))
		offsets := make([]uint32, 0, len(p.rows))
		p.scanNonNull(func(value Value) {
			offsets = append(offsets, uint32(len(values)))
			values = append(values, value.byteArray()...)
		})
		offsets = append(offsets, uint32(len(values)))
		return encoding.ByteArrayValues(values, offsets)

	case FixedLenByteArray:
		length := p.typ.Length()
		values := make([]byte, 0, length*len(p.rows))
		p.scanNonNull(func(value Value) { values = append(values, value.byteArray()...) })
		return encoding.FixedLenByteArrayValues(values, length)

	default:
		return encoding.Values{}
	}
}

func (p *rowBufferPage) scan(f func(Value)) {
	columnIndex := ^int16(p.column)

	for _, row := range p.rows {
		for _, value := range row {
			if value.columnIndex == columnIndex {
				f(value)
			}
		}
	}
}

func (p *rowBufferPage) scanNonNull(f func(Value)) {
	p.scan(func(value Value) {
		if !value.isNull() {
			f(value)
		}
	})
}

type rowBufferPageValueReader struct {
	page        *rowBufferPage
	rowIndex    int
	valueIndex  int
	columnIndex int16
}

func (r *rowBufferPageValueReader) ReadValues(values []Value) (n int, err error) {
	for n < len(values) && r.rowIndex < len(r.page.rows) {
		for n < len(values) && r.valueIndex < len(r.page.rows[r.rowIndex]) {
			if v := r.page.rows[r.rowIndex][r.valueIndex]; v.columnIndex == r.columnIndex {
				values[n] = v
				n++
			}
			r.valueIndex++
		}
		r.rowIndex++
		r.valueIndex = 0
	}
	if r.rowIndex == len(r.page.rows) {
		err = io.EOF
	}
	return n, err
}

type rowBufferRows struct {
	rows   []Row
	index  int
	schema *Schema
}

func (r *rowBufferRows) Close() error {
	r.index = -1
	return nil
}

func (r *rowBufferRows) Schema() *Schema {
	return r.schema
}

func (r *rowBufferRows) SeekToRow(rowIndex int64) error {
	if rowIndex < 0 {
		return ErrSeekOutOfRange
	}

	if r.index < 0 {
		return io.ErrClosedPipe
	}

	maxRowIndex := int64(len(r.rows))
	if rowIndex > maxRowIndex {
		rowIndex = maxRowIndex
	}

	r.index = int(rowIndex)
	return nil
}

func (r *rowBufferRows) ReadRows(rows []Row) (n int, err error) {
	if r.index < 0 {
		return 0, io.EOF
	}

	if n = len(r.rows) - r.index; n > len(rows) {
		n = len(rows)
	}

	for i, row := range r.rows[r.index : r.index+n] {
		rows[i] = append(rows[i][:0], row...)
	}

	if r.index += n; r.index == len(r.rows) {
		err = io.EOF
	}

	return n, err
}

func (r *rowBufferRows) WriteRowsTo(w RowWriter) (int64, error) {
	n, err := w.WriteRows(r.rows[r.index:])
	r.index += n
	return int64(n), err
}

var (
	_ RowGroup       = (*RowBuffer[any])(nil)
	_ RowWriter      = (*RowBuffer[any])(nil)
	_ sort.Interface = (*RowBuffer[any])(nil)

	_ RowWriterTo = (*rowBufferRows)(nil)
)
