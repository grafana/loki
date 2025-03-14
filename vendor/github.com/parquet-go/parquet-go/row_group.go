package parquet

import (
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go/internal/debug"
)

// RowGroup is an interface representing a parquet row group. From the Parquet
// docs, a RowGroup is "a logical horizontal partitioning of the data into rows.
// There is no physical structure that is guaranteed for a row group. A row
// group consists of a column chunk for each column in the dataset."
//
// https://github.com/apache/parquet-format#glossary
type RowGroup interface {
	// Returns the number of rows in the group.
	NumRows() int64

	// Returns the list of column chunks in this row group. The chunks are
	// ordered in the order of leaf columns from the row group's schema.
	//
	// If the underlying implementation is not read-only, the returned
	// parquet.ColumnChunk may implement other interfaces: for example,
	// parquet.ColumnBuffer if the chunk is backed by an in-memory buffer,
	// or typed writer interfaces like parquet.Int32Writer depending on the
	// underlying type of values that can be written to the chunk.
	//
	// As an optimization, the row group may return the same slice across
	// multiple calls to this method. Applications should treat the returned
	// slice as read-only.
	ColumnChunks() []ColumnChunk

	// Returns the schema of rows in the group.
	Schema() *Schema

	// Returns the list of sorting columns describing how rows are sorted in the
	// group.
	//
	// The method will return an empty slice if the rows are not sorted.
	SortingColumns() []SortingColumn

	// Returns a reader exposing the rows of the row group.
	//
	// As an optimization, the returned parquet.Rows object may implement
	// parquet.RowWriterTo, and test the RowWriter it receives for an
	// implementation of the parquet.RowGroupWriter interface.
	//
	// This optimization mechanism is leveraged by the parquet.CopyRows function
	// to skip the generic row-by-row copy algorithm and delegate the copy logic
	// to the parquet.Rows object.
	Rows() Rows
}

// Rows is an interface implemented by row readers returned by calling the Rows
// method of RowGroup instances.
//
// Applications should call Close when they are done using a Rows instance in
// order to release the underlying resources held by the row sequence.
//
// After calling Close, all attempts to read more rows will return io.EOF.
type Rows interface {
	RowReadSeekCloser
	Schema() *Schema
}

// RowGroupReader is an interface implemented by types that expose sequences of
// row groups to the application.
type RowGroupReader interface {
	ReadRowGroup() (RowGroup, error)
}

// RowGroupWriter is an interface implemented by types that allow the program
// to write row groups.
type RowGroupWriter interface {
	WriteRowGroup(RowGroup) (int64, error)
}

// SortingColumn represents a column by which a row group is sorted.
type SortingColumn interface {
	// Returns the path of the column in the row group schema, omitting the name
	// of the root node.
	Path() []string

	// Returns true if the column will sort values in descending order.
	Descending() bool

	// Returns true if the column will put null values at the beginning.
	NullsFirst() bool
}

// Ascending constructs a SortingColumn value which dictates to sort the column
// at the path given as argument in ascending order.
func Ascending(path ...string) SortingColumn { return ascending(path) }

// Descending constructs a SortingColumn value which dictates to sort the column
// at the path given as argument in descending order.
func Descending(path ...string) SortingColumn { return descending(path) }

// NullsFirst wraps the SortingColumn passed as argument so that it instructs
// the row group to place null values first in the column.
func NullsFirst(sortingColumn SortingColumn) SortingColumn { return nullsFirst{sortingColumn} }

type ascending []string

func (asc ascending) String() string   { return fmt.Sprintf("ascending(%s)", columnPath(asc)) }
func (asc ascending) Path() []string   { return asc }
func (asc ascending) Descending() bool { return false }
func (asc ascending) NullsFirst() bool { return false }

type descending []string

func (desc descending) String() string   { return fmt.Sprintf("descending(%s)", columnPath(desc)) }
func (desc descending) Path() []string   { return desc }
func (desc descending) Descending() bool { return true }
func (desc descending) NullsFirst() bool { return false }

type nullsFirst struct{ SortingColumn }

func (nf nullsFirst) String() string   { return fmt.Sprintf("nulls_first+%s", nf.SortingColumn) }
func (nf nullsFirst) NullsFirst() bool { return true }

func searchSortingColumn(sortingColumns []SortingColumn, path columnPath) int {
	// There are usually a few sorting columns in a row group, so the linear
	// scan is the fastest option and works whether the sorting column list
	// is sorted or not. Please revisit this decision if this code path ends
	// up being more costly than necessary.
	for i, sorting := range sortingColumns {
		if path.equal(sorting.Path()) {
			return i
		}
	}
	return len(sortingColumns)
}

func sortingColumnsHavePrefix(sortingColumns, prefix []SortingColumn) bool {
	if len(sortingColumns) < len(prefix) {
		return false
	}
	for i, sortingColumn := range prefix {
		if !sortingColumnsAreEqual(sortingColumns[i], sortingColumn) {
			return false
		}
	}
	return true
}

func sortingColumnsAreEqual(s1, s2 SortingColumn) bool {
	path1 := columnPath(s1.Path())
	path2 := columnPath(s2.Path())
	return path1.equal(path2) && s1.Descending() == s2.Descending() && s1.NullsFirst() == s2.NullsFirst()
}

type rowGroup struct {
	schema  *Schema
	numRows int64
	columns []ColumnChunk
	sorting []SortingColumn
}

func (r *rowGroup) NumRows() int64                  { return r.numRows }
func (r *rowGroup) ColumnChunks() []ColumnChunk     { return r.columns }
func (r *rowGroup) SortingColumns() []SortingColumn { return r.sorting }
func (r *rowGroup) Schema() *Schema                 { return r.schema }
func (r *rowGroup) Rows() Rows                      { return NewRowGroupRowReader(r) }

func AsyncRowGroup(base RowGroup) RowGroup {
	columnChunks := base.ColumnChunks()
	asyncRowGroup := &rowGroup{
		schema:  base.Schema(),
		numRows: base.NumRows(),
		sorting: base.SortingColumns(),
		columns: make([]ColumnChunk, len(columnChunks)),
	}
	asyncColumnChunks := make([]asyncColumnChunk, len(columnChunks))
	for i, columnChunk := range columnChunks {
		asyncColumnChunks[i].ColumnChunk = columnChunk
		asyncRowGroup.columns[i] = &asyncColumnChunks[i]
	}
	return asyncRowGroup
}

type rowGroupRows struct {
	schema   *Schema
	bufsize  int
	buffers  []Value
	columns  []columnChunkRows
	closed   bool
	rowIndex int64
}

type columnChunkRows struct {
	offset int32
	length int32
	reader columnChunkValueReader
}

func (r *rowGroupRows) buffer(i int) []Value {
	j := (i + 0) * r.bufsize
	k := (i + 1) * r.bufsize
	return r.buffers[j:k:k]
}

// / NewRowGroupRowReader constructs a new row reader for the given row group.
func NewRowGroupRowReader(rowGroup RowGroup) Rows {
	return newRowGroupRows(rowGroup.Schema(), rowGroup.ColumnChunks(), defaultValueBufferSize)
}

func newRowGroupRows(schema *Schema, columns []ColumnChunk, bufferSize int) *rowGroupRows {
	r := &rowGroupRows{
		schema:   schema,
		bufsize:  bufferSize,
		buffers:  make([]Value, len(columns)*bufferSize),
		columns:  make([]columnChunkRows, len(columns)),
		rowIndex: -1,
	}

	for i, column := range columns {
		var release func(Page)
		// Only release pages that are not byte array because the values
		// that were read from the page might be retained by the program
		// after calls to ReadRows.
		switch column.Type().Kind() {
		case ByteArray, FixedLenByteArray:
			release = func(Page) {}
		default:
			release = Release
		}
		r.columns[i].reader.release = release
		r.columns[i].reader.pages = column.Pages()
	}

	// This finalizer is used to ensure that the goroutines started by calling
	// init on the underlying page readers will be shutdown in the event that
	// Close isn't called and the rowGroupRows object is garbage collected.
	debug.SetFinalizer(r, func(r *rowGroupRows) { r.Close() })
	return r
}

func (r *rowGroupRows) clear() {
	for i, c := range r.columns {
		r.columns[i] = columnChunkRows{reader: c.reader}
	}
	clear(r.buffers)
}

func (r *rowGroupRows) Reset() {
	for i := range r.columns {
		r.columns[i].reader.Reset()
	}
	r.clear()
}

func (r *rowGroupRows) Close() error {
	var errs []error
	for i := range r.columns {
		c := &r.columns[i]
		c.offset = 0
		c.length = 0
		if err := c.reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	r.clear()
	r.closed = true
	return errors.Join(errs...)
}

func (r *rowGroupRows) SeekToRow(rowIndex int64) error {
	if r.closed {
		return io.ErrClosedPipe
	}
	if rowIndex != r.rowIndex {
		for i := range r.columns {
			if err := r.columns[i].reader.SeekToRow(rowIndex); err != nil {
				return err
			}
		}
		r.clear()
		r.rowIndex = rowIndex
	}
	return nil
}

func (r *rowGroupRows) ReadRows(rows []Row) (int, error) {
	if r.closed {
		return 0, io.EOF
	}

	for rowIndex := range rows {
		rows[rowIndex] = rows[rowIndex][:0]
	}

	// When this is the first call to ReadRows, we issue a seek to the first row
	// because this starts prefetching pages asynchronously on columns.
	//
	// This condition does not apply if SeekToRow was called before ReadRows,
	// only when ReadRows is the very first method called on the row reader.
	if r.rowIndex < 0 {
		if err := r.SeekToRow(0); err != nil {
			return 0, err
		}
	}

	eofCount := 0
	rowCount := 0

readColumnValues:
	for columnIndex := range r.columns {
		c := &r.columns[columnIndex]
		b := r.buffer(columnIndex)
		eof := false

		for rowIndex := range rows {
			numValuesInRow := 1

			for {
				if c.offset == c.length {
					n, err := c.reader.ReadValues(b)
					c.offset = 0
					c.length = int32(n)

					if n == 0 {
						if err == io.EOF {
							eof = true
							eofCount++
							break
						}
						return 0, err
					}
				}

				values := b[c.offset:c.length:c.length]
				for numValuesInRow < len(values) && values[numValuesInRow].repetitionLevel != 0 {
					numValuesInRow++
				}
				if numValuesInRow == 0 {
					break
				}

				rows[rowIndex] = append(rows[rowIndex], values[:numValuesInRow]...)
				rowCount = max(rowCount, rowIndex+1)
				c.offset += int32(numValuesInRow)

				if numValuesInRow != len(values) {
					break
				}
				if eof {
					continue readColumnValues
				}
				numValuesInRow = 0
			}
		}
	}

	var err error
	if eofCount > 0 {
		err = io.EOF
	}
	r.rowIndex += int64(rowCount)
	return rowCount, err
}

func (r *rowGroupRows) Schema() *Schema {
	return r.schema
}

type seekRowGroup struct {
	base    RowGroup
	seek    int64
	columns []ColumnChunk
}

func (g *seekRowGroup) NumRows() int64 {
	return g.base.NumRows() - g.seek
}

func (g *seekRowGroup) ColumnChunks() []ColumnChunk {
	return g.columns
}

func (g *seekRowGroup) Schema() *Schema {
	return g.base.Schema()
}

func (g *seekRowGroup) SortingColumns() []SortingColumn {
	return g.base.SortingColumns()
}

func (g *seekRowGroup) Rows() Rows {
	rows := g.base.Rows()
	rows.SeekToRow(g.seek)
	return rows
}

type seekColumnChunk struct {
	base ColumnChunk
	seek int64
}

func (c *seekColumnChunk) Type() Type {
	return c.base.Type()
}

func (c *seekColumnChunk) Column() int {
	return c.base.Column()
}

func (c *seekColumnChunk) Pages() Pages {
	pages := c.base.Pages()
	pages.SeekToRow(c.seek)
	return pages
}

func (c *seekColumnChunk) ColumnIndex() (ColumnIndex, error) {
	return c.base.ColumnIndex()
}

func (c *seekColumnChunk) OffsetIndex() (OffsetIndex, error) {
	return c.base.OffsetIndex()
}

func (c *seekColumnChunk) BloomFilter() BloomFilter {
	return c.base.BloomFilter()
}

func (c *seekColumnChunk) NumValues() int64 {
	return c.base.NumValues()
}

type emptyRowGroup struct {
	schema  *Schema
	columns []ColumnChunk
}

func newEmptyRowGroup(schema *Schema) *emptyRowGroup {
	columns := schema.Columns()
	rowGroup := &emptyRowGroup{
		schema:  schema,
		columns: make([]ColumnChunk, len(columns)),
	}
	emptyColumnChunks := make([]emptyColumnChunk, len(columns))
	for i, column := range schema.Columns() {
		leaf, _ := schema.Lookup(column...)
		emptyColumnChunks[i].typ = leaf.Node.Type()
		emptyColumnChunks[i].column = int16(leaf.ColumnIndex)
		rowGroup.columns[i] = &emptyColumnChunks[i]
	}
	return rowGroup
}

func (g *emptyRowGroup) NumRows() int64                  { return 0 }
func (g *emptyRowGroup) ColumnChunks() []ColumnChunk     { return g.columns }
func (g *emptyRowGroup) Schema() *Schema                 { return g.schema }
func (g *emptyRowGroup) SortingColumns() []SortingColumn { return nil }
func (g *emptyRowGroup) Rows() Rows                      { return emptyRows{g.schema} }

type emptyColumnChunk struct {
	typ    Type
	column int16
}

func (c *emptyColumnChunk) Type() Type                        { return c.typ }
func (c *emptyColumnChunk) Column() int                       { return int(c.column) }
func (c *emptyColumnChunk) Pages() Pages                      { return emptyPages{} }
func (c *emptyColumnChunk) ColumnIndex() (ColumnIndex, error) { return emptyColumnIndex{}, nil }
func (c *emptyColumnChunk) OffsetIndex() (OffsetIndex, error) { return emptyOffsetIndex{}, nil }
func (c *emptyColumnChunk) BloomFilter() BloomFilter          { return emptyBloomFilter{} }
func (c *emptyColumnChunk) NumValues() int64                  { return 0 }

type emptyBloomFilter struct{}

func (emptyBloomFilter) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (emptyBloomFilter) Size() int64                       { return 0 }
func (emptyBloomFilter) Check(Value) (bool, error)         { return false, nil }

type emptyRows struct{ schema *Schema }

func (r emptyRows) Close() error                         { return nil }
func (r emptyRows) Schema() *Schema                      { return r.schema }
func (r emptyRows) ReadRows([]Row) (int, error)          { return 0, io.EOF }
func (r emptyRows) SeekToRow(int64) error                { return nil }
func (r emptyRows) WriteRowsTo(RowWriter) (int64, error) { return 0, nil }

type emptyPages struct{}

func (emptyPages) ReadPage() (Page, error) { return nil, io.EOF }
func (emptyPages) SeekToRow(int64) error   { return nil }
func (emptyPages) Close() error            { return nil }

var (
	_ RowReaderWithSchema = (*rowGroupRows)(nil)
	//_ RowWriterTo         = (*rowGroupRows)(nil)

	_ RowReaderWithSchema = emptyRows{}
	_ RowWriterTo         = emptyRows{}
)
