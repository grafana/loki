package parquet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"reflect"
	"sort"
	"time"
	"unsafe"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/internal/bitpack"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

// ColumnBuffer is an interface representing columns of a row group.
//
// ColumnBuffer implements sort.Interface as a way to support reordering the
// rows that have been written to it.
//
// The current implementation has a limitation which prevents applications from
// providing custom versions of this interface because it contains unexported
// methods. The only way to create ColumnBuffer values is to call the
// NewColumnBuffer of Type instances. This limitation may be lifted in future
// releases.
type ColumnBuffer interface {
	// Exposes a read-only view of the column buffer.
	ColumnChunk

	// The column implements ValueReaderAt as a mechanism to read values at
	// specific locations within the buffer.
	ValueReaderAt

	// The column implements ValueWriter as a mechanism to optimize the copy
	// of values into the buffer in contexts where the row information is
	// provided by the values because the repetition and definition levels
	// are set.
	ValueWriter

	// For indexed columns, returns the underlying dictionary holding the column
	// values. If the column is not indexed, nil is returned.
	Dictionary() Dictionary

	// Returns a copy of the column. The returned copy shares no memory with
	// the original, mutations of either column will not modify the other.
	Clone() ColumnBuffer

	// Returns the column as a Page.
	Page() Page

	// Clears all rows written to the column.
	Reset()

	// Returns the current capacity of the column (rows).
	Cap() int

	// Returns the number of rows currently written to the column.
	Len() int

	// Compares rows at index i and j and reports whether i < j.
	Less(i, j int) bool

	// Swaps rows at index i and j.
	Swap(i, j int)

	// Returns the size of the column buffer in bytes.
	Size() int64

	// This method is employed to write rows from arrays of Go values into the
	// column buffer. The method is currently unexported because it uses unsafe
	// APIs which would be difficult for applications to leverage, increasing
	// the risk of introducing bugs in the code. As a consequence, applications
	// cannot use custom implementations of the ColumnBuffer interface since
	// they cannot declare an unexported method that would match this signature.
	// It means that in order to create a ColumnBuffer value, programs need to
	// go through a call to NewColumnBuffer on a Type instance. We make this
	// trade off for now as it is preferrable to optimize for safety over
	// extensibility in the public APIs, we might revisit in the future if we
	// learn about valid use cases for custom column buffer types.
	writeValues(rows sparse.Array, levels columnLevels)
}

type columnLevels struct {
	repetitionDepth byte
	repetitionLevel byte
	definitionLevel byte
}

func columnIndexOfNullable(base ColumnBuffer, maxDefinitionLevel byte, definitionLevels []byte) (ColumnIndex, error) {
	index, err := base.ColumnIndex()
	if err != nil {
		return nil, err
	}
	return &nullableColumnIndex{
		ColumnIndex:        index,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
	}, nil
}

type nullableColumnIndex struct {
	ColumnIndex
	maxDefinitionLevel byte
	definitionLevels   []byte
}

func (index *nullableColumnIndex) NullPage(i int) bool {
	return index.NullCount(i) == int64(len(index.definitionLevels))
}

func (index *nullableColumnIndex) NullCount(i int) int64 {
	return int64(countLevelsNotEqual(index.definitionLevels, index.maxDefinitionLevel))
}

type nullOrdering func(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool

func nullsGoFirst(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool {
	if definitionLevel1 != maxDefinitionLevel {
		return definitionLevel2 == maxDefinitionLevel
	} else {
		return definitionLevel2 == maxDefinitionLevel && column.Less(i, j)
	}
}

func nullsGoLast(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool {
	return definitionLevel1 == maxDefinitionLevel && (definitionLevel2 != maxDefinitionLevel || column.Less(i, j))
}

// reversedColumnBuffer is an adapter of ColumnBuffer which inverses the order
// in which rows are ordered when the column gets sorted.
//
// This type is used when buffers are constructed with sorting columns ordering
// values in descending order.
type reversedColumnBuffer struct{ ColumnBuffer }

func (col *reversedColumnBuffer) Less(i, j int) bool { return col.ColumnBuffer.Less(j, i) }

// optionalColumnBuffer is an implementation of the ColumnBuffer interface used
// as a wrapper to an underlying ColumnBuffer to manage the creation of
// definition levels.
//
// Null values are not written to the underlying column; instead, the buffer
// tracks offsets of row values in the column, null row values are represented
// by the value -1 and a definition level less than the max.
//
// This column buffer type is used for all leaf columns that have a non-zero
// max definition level and a zero repetition level, which may be because the
// column or one of its parent(s) are marked optional.
type optionalColumnBuffer struct {
	base               ColumnBuffer
	reordered          bool
	maxDefinitionLevel byte
	rows               []int32
	sortIndex          []int32
	definitionLevels   []byte
	nullOrdering       nullOrdering
}

func newOptionalColumnBuffer(base ColumnBuffer, maxDefinitionLevel byte, nullOrdering nullOrdering) *optionalColumnBuffer {
	n := base.Cap()
	return &optionalColumnBuffer{
		base:               base,
		maxDefinitionLevel: maxDefinitionLevel,
		rows:               make([]int32, 0, n),
		definitionLevels:   make([]byte, 0, n),
		nullOrdering:       nullOrdering,
	}
}

func (col *optionalColumnBuffer) Clone() ColumnBuffer {
	return &optionalColumnBuffer{
		base:               col.base.Clone(),
		reordered:          col.reordered,
		maxDefinitionLevel: col.maxDefinitionLevel,
		rows:               append([]int32{}, col.rows...),
		definitionLevels:   append([]byte{}, col.definitionLevels...),
		nullOrdering:       col.nullOrdering,
	}
}

func (col *optionalColumnBuffer) Type() Type {
	return col.base.Type()
}

func (col *optionalColumnBuffer) NumValues() int64 {
	return int64(len(col.definitionLevels))
}

func (col *optionalColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return columnIndexOfNullable(col.base, col.maxDefinitionLevel, col.definitionLevels)
}

func (col *optionalColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return col.base.OffsetIndex()
}

func (col *optionalColumnBuffer) BloomFilter() BloomFilter {
	return col.base.BloomFilter()
}

func (col *optionalColumnBuffer) Dictionary() Dictionary {
	return col.base.Dictionary()
}

func (col *optionalColumnBuffer) Column() int {
	return col.base.Column()
}

func (col *optionalColumnBuffer) Pages() Pages {
	return onePage(col.Page())
}

func (col *optionalColumnBuffer) Page() Page {
	// No need for any cyclic sorting if the rows have not been reordered.
	// This case is also important because the cyclic sorting modifies the
	// buffer which makes it unsafe to read the buffer concurrently.
	if col.reordered {
		numNulls := countLevelsNotEqual(col.definitionLevels, col.maxDefinitionLevel)
		numValues := len(col.rows) - numNulls

		if numValues > 0 {
			if cap(col.sortIndex) < numValues {
				col.sortIndex = make([]int32, numValues)
			}
			sortIndex := col.sortIndex[:numValues]
			i := 0
			for _, j := range col.rows {
				if j >= 0 {
					sortIndex[j] = int32(i)
					i++
				}
			}

			// Cyclic sort: O(N)
			for i := range sortIndex {
				for j := int(sortIndex[i]); i != j; j = int(sortIndex[i]) {
					col.base.Swap(i, j)
					sortIndex[i], sortIndex[j] = sortIndex[j], sortIndex[i]
				}
			}
		}

		i := 0
		for _, r := range col.rows {
			if r >= 0 {
				col.rows[i] = int32(i)
				i++
			}
		}

		col.reordered = false
	}

	return newOptionalPage(col.base.Page(), col.maxDefinitionLevel, col.definitionLevels)
}

func (col *optionalColumnBuffer) Reset() {
	col.base.Reset()
	col.rows = col.rows[:0]
	col.definitionLevels = col.definitionLevels[:0]
}

func (col *optionalColumnBuffer) Size() int64 {
	return int64(4*len(col.rows)+4*len(col.sortIndex)+len(col.definitionLevels)) + col.base.Size()
}

func (col *optionalColumnBuffer) Cap() int { return cap(col.rows) }

func (col *optionalColumnBuffer) Len() int { return len(col.rows) }

func (col *optionalColumnBuffer) Less(i, j int) bool {
	return col.nullOrdering(
		col.base,
		int(col.rows[i]),
		int(col.rows[j]),
		col.maxDefinitionLevel,
		col.definitionLevels[i],
		col.definitionLevels[j],
	)
}

func (col *optionalColumnBuffer) Swap(i, j int) {
	// Because the underlying column does not contain null values, we cannot
	// swap its values at indexes i and j. We swap the row indexes only, then
	// reorder the underlying buffer using a cyclic sort when the buffer is
	// materialized into a page view.
	col.reordered = true
	col.rows[i], col.rows[j] = col.rows[j], col.rows[i]
	col.definitionLevels[i], col.definitionLevels[j] = col.definitionLevels[j], col.definitionLevels[i]
}

func (col *optionalColumnBuffer) WriteValues(values []Value) (n int, err error) {
	rowIndex := int32(col.base.Len())

	for n < len(values) {
		// Collect index range of contiguous null values, from i to n. If this
		// for loop exhausts the values, all remaining if statements and for
		// loops will be no-ops and the loop will terminate.
		i := n
		for n < len(values) && values[n].definitionLevel != col.maxDefinitionLevel {
			n++
		}

		// Write the contiguous null values up until the first non-null value
		// obtained in the for loop above.
		for _, v := range values[i:n] {
			col.rows = append(col.rows, -1)
			col.definitionLevels = append(col.definitionLevels, v.definitionLevel)
		}

		// Collect index range of contiguous non-null values, from i to n.
		i = n
		for n < len(values) && values[n].definitionLevel == col.maxDefinitionLevel {
			n++
		}

		// As long as i < n we have non-null values still to write. It is
		// possible that we just exhausted the input values in which case i == n
		// and the outer for loop will terminate.
		if i < n {
			count, err := col.base.WriteValues(values[i:n])
			col.definitionLevels = appendLevel(col.definitionLevels, col.maxDefinitionLevel, count)

			for count > 0 {
				col.rows = append(col.rows, rowIndex)
				rowIndex++
				count--
			}

			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

func (col *optionalColumnBuffer) writeValues(rows sparse.Array, levels columnLevels) {
	// The row count is zero when writing an null optional value, in which case
	// we still need to output a row to the buffer to record the definition
	// level.
	if rows.Len() == 0 {
		col.definitionLevels = append(col.definitionLevels, levels.definitionLevel)
		col.rows = append(col.rows, -1)
		return
	}

	col.definitionLevels = appendLevel(col.definitionLevels, levels.definitionLevel, rows.Len())

	i := len(col.rows)
	j := len(col.rows) + rows.Len()

	if j <= cap(col.rows) {
		col.rows = col.rows[:j]
	} else {
		tmp := make([]int32, j, 2*j)
		copy(tmp, col.rows)
		col.rows = tmp
	}

	if levels.definitionLevel != col.maxDefinitionLevel {
		broadcastValueInt32(col.rows[i:], -1)
	} else {
		broadcastRangeInt32(col.rows[i:], int32(col.base.Len()))
		col.base.writeValues(rows, levels)
	}
}

func (col *optionalColumnBuffer) ReadValuesAt(values []Value, offset int64) (int, error) {
	length := int64(len(col.definitionLevels))
	if offset < 0 {
		return 0, errRowIndexOutOfBounds(offset, length)
	}
	if offset >= length {
		return 0, io.EOF
	}
	if length -= offset; length < int64(len(values)) {
		values = values[:length]
	}

	numNulls1 := int64(countLevelsNotEqual(col.definitionLevels[:offset], col.maxDefinitionLevel))
	numNulls2 := int64(countLevelsNotEqual(col.definitionLevels[offset:offset+length], col.maxDefinitionLevel))

	if numNulls2 < length {
		n, err := col.base.ReadValuesAt(values[:length-numNulls2], offset-numNulls1)
		if err != nil {
			return n, err
		}
	}

	if numNulls2 > 0 {
		columnIndex := ^int16(col.Column())
		i := numNulls2 - 1
		j := length - 1
		definitionLevels := col.definitionLevels[offset : offset+length]
		maxDefinitionLevel := col.maxDefinitionLevel

		for n := len(definitionLevels) - 1; n >= 0 && j > i; n-- {
			if definitionLevels[n] != maxDefinitionLevel {
				values[j] = Value{definitionLevel: definitionLevels[n], columnIndex: columnIndex}
			} else {
				values[j] = values[i]
				i--
			}
			j--
		}
	}

	return int(length), nil
}

// repeatedColumnBuffer is an implementation of the ColumnBuffer interface used
// as a wrapper to an underlying ColumnBuffer to manage the creation of
// repetition levels, definition levels, and map rows to the region of the
// underlying buffer that contains their sequence of values.
//
// Null values are not written to the underlying column; instead, the buffer
// tracks offsets of row values in the column, null row values are represented
// by the value -1 and a definition level less than the max.
//
// This column buffer type is used for all leaf columns that have a non-zero
// max repetition level, which may be because the column or one of its parent(s)
// are marked repeated.
type repeatedColumnBuffer struct {
	base               ColumnBuffer
	reordered          bool
	maxRepetitionLevel byte
	maxDefinitionLevel byte
	rows               []offsetMapping
	repetitionLevels   []byte
	definitionLevels   []byte
	buffer             []Value
	reordering         *repeatedColumnBuffer
	nullOrdering       nullOrdering
}

// The offsetMapping type maps the logical offset of rows within the repetition
// and definition levels, to the base offsets in the underlying column buffers
// where the non-null values have been written.
type offsetMapping struct {
	offset     uint32
	baseOffset uint32
}

func newRepeatedColumnBuffer(base ColumnBuffer, maxRepetitionLevel, maxDefinitionLevel byte, nullOrdering nullOrdering) *repeatedColumnBuffer {
	n := base.Cap()
	return &repeatedColumnBuffer{
		base:               base,
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		rows:               make([]offsetMapping, 0, n/8),
		repetitionLevels:   make([]byte, 0, n),
		definitionLevels:   make([]byte, 0, n),
		nullOrdering:       nullOrdering,
	}
}

func (col *repeatedColumnBuffer) Clone() ColumnBuffer {
	return &repeatedColumnBuffer{
		base:               col.base.Clone(),
		reordered:          col.reordered,
		maxRepetitionLevel: col.maxRepetitionLevel,
		maxDefinitionLevel: col.maxDefinitionLevel,
		rows:               append([]offsetMapping{}, col.rows...),
		repetitionLevels:   append([]byte{}, col.repetitionLevels...),
		definitionLevels:   append([]byte{}, col.definitionLevels...),
		nullOrdering:       col.nullOrdering,
	}
}

func (col *repeatedColumnBuffer) Type() Type {
	return col.base.Type()
}

func (col *repeatedColumnBuffer) NumValues() int64 {
	return int64(len(col.definitionLevels))
}

func (col *repeatedColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return columnIndexOfNullable(col.base, col.maxDefinitionLevel, col.definitionLevels)
}

func (col *repeatedColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return col.base.OffsetIndex()
}

func (col *repeatedColumnBuffer) BloomFilter() BloomFilter {
	return col.base.BloomFilter()
}

func (col *repeatedColumnBuffer) Dictionary() Dictionary {
	return col.base.Dictionary()
}

func (col *repeatedColumnBuffer) Column() int {
	return col.base.Column()
}

func (col *repeatedColumnBuffer) Pages() Pages {
	return onePage(col.Page())
}

func (col *repeatedColumnBuffer) Page() Page {
	if col.reordered {
		if col.reordering == nil {
			col.reordering = col.Clone().(*repeatedColumnBuffer)
		}

		column := col.reordering
		column.Reset()
		maxNumValues := 0
		defer func() {
			clearValues(col.buffer[:maxNumValues])
		}()

		baseOffset := 0

		for _, row := range col.rows {
			rowOffset := int(row.offset)
			rowLength := repeatedRowLength(col.repetitionLevels[rowOffset:])
			numNulls := countLevelsNotEqual(col.definitionLevels[rowOffset:rowOffset+rowLength], col.maxDefinitionLevel)
			numValues := rowLength - numNulls

			if numValues > 0 {
				if numValues > cap(col.buffer) {
					col.buffer = make([]Value, numValues)
				} else {
					col.buffer = col.buffer[:numValues]
				}
				n, err := col.base.ReadValuesAt(col.buffer, int64(row.baseOffset))
				if err != nil && n < numValues {
					return newErrorPage(col.Type(), col.Column(), "reordering rows of repeated column: %w", err)
				}
				if _, err := column.base.WriteValues(col.buffer); err != nil {
					return newErrorPage(col.Type(), col.Column(), "reordering rows of repeated column: %w", err)
				}
				if numValues > maxNumValues {
					maxNumValues = numValues
				}
			}

			column.rows = append(column.rows, offsetMapping{
				offset:     uint32(len(column.repetitionLevels)),
				baseOffset: uint32(baseOffset),
			})

			column.repetitionLevels = append(column.repetitionLevels, col.repetitionLevels[rowOffset:rowOffset+rowLength]...)
			column.definitionLevels = append(column.definitionLevels, col.definitionLevels[rowOffset:rowOffset+rowLength]...)
			baseOffset += numValues
		}

		col.swapReorderingBuffer(column)
		col.reordered = false
	}

	return newRepeatedPage(
		col.base.Page(),
		col.maxRepetitionLevel,
		col.maxDefinitionLevel,
		col.repetitionLevels,
		col.definitionLevels,
	)
}

func (col *repeatedColumnBuffer) swapReorderingBuffer(buf *repeatedColumnBuffer) {
	col.base, buf.base = buf.base, col.base
	col.rows, buf.rows = buf.rows, col.rows
	col.repetitionLevels, buf.repetitionLevels = buf.repetitionLevels, col.repetitionLevels
	col.definitionLevels, buf.definitionLevels = buf.definitionLevels, col.definitionLevels
}

func (col *repeatedColumnBuffer) Reset() {
	col.base.Reset()
	col.rows = col.rows[:0]
	col.repetitionLevels = col.repetitionLevels[:0]
	col.definitionLevels = col.definitionLevels[:0]
}

func (col *repeatedColumnBuffer) Size() int64 {
	return int64(8*len(col.rows)+len(col.repetitionLevels)+len(col.definitionLevels)) + col.base.Size()
}

func (col *repeatedColumnBuffer) Cap() int { return cap(col.rows) }

func (col *repeatedColumnBuffer) Len() int { return len(col.rows) }

func (col *repeatedColumnBuffer) Less(i, j int) bool {
	row1 := col.rows[i]
	row2 := col.rows[j]
	less := col.nullOrdering
	row1Length := repeatedRowLength(col.repetitionLevels[row1.offset:])
	row2Length := repeatedRowLength(col.repetitionLevels[row2.offset:])

	for k := 0; k < row1Length && k < row2Length; k++ {
		x := int(row1.baseOffset)
		y := int(row2.baseOffset)
		definitionLevel1 := col.definitionLevels[int(row1.offset)+k]
		definitionLevel2 := col.definitionLevels[int(row2.offset)+k]
		switch {
		case less(col.base, x, y, col.maxDefinitionLevel, definitionLevel1, definitionLevel2):
			return true
		case less(col.base, y, x, col.maxDefinitionLevel, definitionLevel2, definitionLevel1):
			return false
		}
	}

	return row1Length < row2Length
}

func (col *repeatedColumnBuffer) Swap(i, j int) {
	// Because the underlying column does not contain null values, and may hold
	// an arbitrary number of values per row, we cannot swap its values at
	// indexes i and j. We swap the row indexes only, then reorder the base
	// column buffer when its view is materialized into a page by creating a
	// copy and writing rows back to it following the order of rows in the
	// repeated column buffer.
	col.reordered = true
	col.rows[i], col.rows[j] = col.rows[j], col.rows[i]
}

func (col *repeatedColumnBuffer) WriteValues(values []Value) (numValues int, err error) {
	maxRowLen := 0
	defer func() {
		clearValues(col.buffer[:maxRowLen])
	}()

	for i := 0; i < len(values); {
		j := i

		if values[j].repetitionLevel == 0 {
			j++
		}

		for j < len(values) && values[j].repetitionLevel != 0 {
			j++
		}

		if err := col.writeRow(values[i:j]); err != nil {
			return numValues, err
		}

		if len(col.buffer) > maxRowLen {
			maxRowLen = len(col.buffer)
		}

		numValues += j - i
		i = j
	}

	return numValues, nil
}

func (col *repeatedColumnBuffer) writeRow(row []Value) error {
	col.buffer = col.buffer[:0]

	for _, v := range row {
		if v.definitionLevel == col.maxDefinitionLevel {
			col.buffer = append(col.buffer, v)
		}
	}

	baseOffset := col.base.NumValues()
	if len(col.buffer) > 0 {
		if _, err := col.base.WriteValues(col.buffer); err != nil {
			return err
		}
	}

	if row[0].repetitionLevel == 0 {
		col.rows = append(col.rows, offsetMapping{
			offset:     uint32(len(col.repetitionLevels)),
			baseOffset: uint32(baseOffset),
		})
	}

	for _, v := range row {
		col.repetitionLevels = append(col.repetitionLevels, v.repetitionLevel)
		col.definitionLevels = append(col.definitionLevels, v.definitionLevel)
	}

	return nil
}

func (col *repeatedColumnBuffer) writeValues(row sparse.Array, levels columnLevels) {
	if levels.repetitionLevel == 0 {
		col.rows = append(col.rows, offsetMapping{
			offset:     uint32(len(col.repetitionLevels)),
			baseOffset: uint32(col.base.NumValues()),
		})
	}

	if row.Len() == 0 {
		col.repetitionLevels = append(col.repetitionLevels, levels.repetitionLevel)
		col.definitionLevels = append(col.definitionLevels, levels.definitionLevel)
		return
	}

	col.repetitionLevels = appendLevel(col.repetitionLevels, levels.repetitionLevel, row.Len())
	col.definitionLevels = appendLevel(col.definitionLevels, levels.definitionLevel, row.Len())

	if levels.definitionLevel == col.maxDefinitionLevel {
		col.base.writeValues(row, levels)
	}
}

func (col *repeatedColumnBuffer) ReadValuesAt(values []Value, offset int64) (int, error) {
	// TODO:
	panic("NOT IMPLEMENTED")
}

// repeatedRowLength gives the length of the repeated row starting at the
// beginning of the repetitionLevels slice.
func repeatedRowLength(repetitionLevels []byte) int {
	// If a repetition level exists, at least one value is required to represent
	// the column.
	if len(repetitionLevels) > 0 {
		// The subsequent levels will represent the start of a new record when
		// they go back to zero.
		if i := bytes.IndexByte(repetitionLevels[1:], 0); i >= 0 {
			return i + 1
		}
	}
	return len(repetitionLevels)
}

// =============================================================================
// The types below are in-memory implementations of the ColumnBuffer interface
// for each parquet type.
//
// These column buffers are created by calling NewColumnBuffer on parquet.Type
// instances; each parquet type manages to construct column buffers of the
// appropriate type, which ensures that we are packing as many values as we
// can in memory.
//
// See Type.NewColumnBuffer for details about how these types get created.
// =============================================================================

type booleanColumnBuffer struct{ booleanPage }

func newBooleanColumnBuffer(typ Type, columnIndex int16, numValues int32) *booleanColumnBuffer {
	// Boolean values are bit-packed, we can fit up to 8 values per byte.
	bufferSize := (numValues + 7) / 8
	return &booleanColumnBuffer{
		booleanPage: booleanPage{
			typ:         typ,
			bits:        make([]byte, 0, bufferSize),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *booleanColumnBuffer) Clone() ColumnBuffer {
	return &booleanColumnBuffer{
		booleanPage: booleanPage{
			typ:         col.typ,
			bits:        append([]byte{}, col.bits...),
			offset:      col.offset,
			numValues:   col.numValues,
			columnIndex: col.columnIndex,
		},
	}
}

func (col *booleanColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return booleanColumnIndex{&col.booleanPage}, nil
}

func (col *booleanColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return booleanOffsetIndex{&col.booleanPage}, nil
}

func (col *booleanColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *booleanColumnBuffer) Dictionary() Dictionary { return nil }

func (col *booleanColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *booleanColumnBuffer) Page() Page { return &col.booleanPage }

func (col *booleanColumnBuffer) Reset() {
	col.bits = col.bits[:0]
	col.offset = 0
	col.numValues = 0
}

func (col *booleanColumnBuffer) Cap() int { return 8 * cap(col.bits) }

func (col *booleanColumnBuffer) Len() int { return int(col.numValues) }

func (col *booleanColumnBuffer) Less(i, j int) bool {
	a := col.valueAt(i)
	b := col.valueAt(j)
	return a != b && !a
}

func (col *booleanColumnBuffer) valueAt(i int) bool {
	j := uint32(i) / 8
	k := uint32(i) % 8
	return ((col.bits[j] >> k) & 1) != 0
}

func (col *booleanColumnBuffer) setValueAt(i int, v bool) {
	// `offset` is always zero in the page of a column buffer
	j := uint32(i) / 8
	k := uint32(i) % 8
	x := byte(0)
	if v {
		x = 1
	}
	col.bits[j] = (col.bits[j] & ^(1 << k)) | (x << k)
}

func (col *booleanColumnBuffer) Swap(i, j int) {
	a := col.valueAt(i)
	b := col.valueAt(j)
	col.setValueAt(i, b)
	col.setValueAt(j, a)
}

func (col *booleanColumnBuffer) WriteBooleans(values []bool) (int, error) {
	col.writeValues(sparse.MakeBoolArray(values).UnsafeArray(), columnLevels{})
	return len(values), nil
}

func (col *booleanColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfBool), columnLevels{})
	return len(values), nil
}

func (col *booleanColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	numBytes := bitpack.ByteCount(uint(col.numValues) + uint(rows.Len()))
	if cap(col.bits) < numBytes {
		col.bits = append(make([]byte, 0, max(numBytes, 2*cap(col.bits))), col.bits...)
	}
	col.bits = col.bits[:numBytes]
	i := 0
	r := 8 - (int(col.numValues) % 8)
	bytes := rows.Uint8Array()

	if r <= bytes.Len() {
		// First we attempt to write enough bits to align the number of values
		// in the column buffer on 8 bytes. After this step the next bit should
		// be written at the zero'th index of a byte of the buffer.
		if r < 8 {
			var b byte
			for i < r {
				v := bytes.Index(i)
				b |= (v & 1) << uint(i)
				i++
			}
			x := uint(col.numValues) / 8
			y := uint(col.numValues) % 8
			col.bits[x] = (b << y) | (col.bits[x] & ^(0xFF << y))
			col.numValues += int32(i)
		}

		if n := ((bytes.Len() - i) / 8) * 8; n > 0 {
			// At this stage, we know that that we have at least 8 bits to write
			// and the bits will be aligned on the address of a byte in the
			// output buffer. We can work on 8 values per loop iteration,
			// packing them into a single byte and writing it to the output
			// buffer. This effectively reduces by 87.5% the number of memory
			// stores that the program needs to perform to generate the values.
			i += sparse.GatherBits(col.bits[col.numValues/8:], bytes.Slice(i, i+n))
			col.numValues += int32(n)
		}
	}

	for i < bytes.Len() {
		x := uint(col.numValues) / 8
		y := uint(col.numValues) % 8
		b := bytes.Index(i)
		col.bits[x] = ((b & 1) << y) | (col.bits[x] & ^(1 << y))
		col.numValues++
		i++
	}

	col.bits = col.bits[:bitpack.ByteCount(uint(col.numValues))]
}

func (col *booleanColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(col.numValues))
	case i >= int(col.numValues):
		return 0, io.EOF
	default:
		for n < len(values) && i < int(col.numValues) {
			values[n] = col.makeValue(col.valueAt(i))
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type int32ColumnBuffer struct{ int32Page }

func newInt32ColumnBuffer(typ Type, columnIndex int16, numValues int32) *int32ColumnBuffer {
	return &int32ColumnBuffer{
		int32Page: int32Page{
			typ:         typ,
			values:      make([]int32, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *int32ColumnBuffer) Clone() ColumnBuffer {
	return &int32ColumnBuffer{
		int32Page: int32Page{
			typ:         col.typ,
			values:      append([]int32{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *int32ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return int32ColumnIndex{&col.int32Page}, nil
}

func (col *int32ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return int32OffsetIndex{&col.int32Page}, nil
}

func (col *int32ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *int32ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *int32ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *int32ColumnBuffer) Page() Page { return &col.int32Page }

func (col *int32ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *int32ColumnBuffer) Cap() int { return cap(col.values) }

func (col *int32ColumnBuffer) Len() int { return len(col.values) }

func (col *int32ColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *int32ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *int32ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write INT32 values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[int32](b)...)
	return len(b), nil
}

func (col *int32ColumnBuffer) WriteInt32s(values []int32) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *int32ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU32), columnLevels{})
	return len(values), nil
}

func (col *int32ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]int32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherInt32(col.values[n:], rows.Int32Array())

}

func (col *int32ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type int64ColumnBuffer struct{ int64Page }

func newInt64ColumnBuffer(typ Type, columnIndex int16, numValues int32) *int64ColumnBuffer {
	return &int64ColumnBuffer{
		int64Page: int64Page{
			typ:         typ,
			values:      make([]int64, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *int64ColumnBuffer) Clone() ColumnBuffer {
	return &int64ColumnBuffer{
		int64Page: int64Page{
			typ:         col.typ,
			values:      append([]int64{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *int64ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return int64ColumnIndex{&col.int64Page}, nil
}

func (col *int64ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return int64OffsetIndex{&col.int64Page}, nil
}

func (col *int64ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *int64ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *int64ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *int64ColumnBuffer) Page() Page { return &col.int64Page }

func (col *int64ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *int64ColumnBuffer) Cap() int { return cap(col.values) }

func (col *int64ColumnBuffer) Len() int { return len(col.values) }

func (col *int64ColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *int64ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *int64ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write INT64 values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[int64](b)...)
	return len(b), nil
}

func (col *int64ColumnBuffer) WriteInt64s(values []int64) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *int64ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU64), columnLevels{})
	return len(values), nil
}

func (col *int64ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]int64, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherInt64(col.values[n:], rows.Int64Array())
}

func (col *int64ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type int96ColumnBuffer struct{ int96Page }

func newInt96ColumnBuffer(typ Type, columnIndex int16, numValues int32) *int96ColumnBuffer {
	return &int96ColumnBuffer{
		int96Page: int96Page{
			typ:         typ,
			values:      make([]deprecated.Int96, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *int96ColumnBuffer) Clone() ColumnBuffer {
	return &int96ColumnBuffer{
		int96Page: int96Page{
			typ:         col.typ,
			values:      append([]deprecated.Int96{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *int96ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return int96ColumnIndex{&col.int96Page}, nil
}

func (col *int96ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return int96OffsetIndex{&col.int96Page}, nil
}

func (col *int96ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *int96ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *int96ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *int96ColumnBuffer) Page() Page { return &col.int96Page }

func (col *int96ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *int96ColumnBuffer) Cap() int { return cap(col.values) }

func (col *int96ColumnBuffer) Len() int { return len(col.values) }

func (col *int96ColumnBuffer) Less(i, j int) bool { return col.values[i].Less(col.values[j]) }

func (col *int96ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *int96ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 12) != 0 {
		return 0, fmt.Errorf("cannot write INT96 values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[deprecated.Int96](b)...)
	return len(b), nil
}

func (col *int96ColumnBuffer) WriteInt96s(values []deprecated.Int96) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *int96ColumnBuffer) WriteValues(values []Value) (int, error) {
	for _, v := range values {
		col.values = append(col.values, v.Int96())
	}
	return len(values), nil
}

func (col *int96ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	for i := 0; i < rows.Len(); i++ {
		p := rows.Index(i)
		col.values = append(col.values, *(*deprecated.Int96)(p))
	}
}

func (col *int96ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type floatColumnBuffer struct{ floatPage }

func newFloatColumnBuffer(typ Type, columnIndex int16, numValues int32) *floatColumnBuffer {
	return &floatColumnBuffer{
		floatPage: floatPage{
			typ:         typ,
			values:      make([]float32, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *floatColumnBuffer) Clone() ColumnBuffer {
	return &floatColumnBuffer{
		floatPage: floatPage{
			typ:         col.typ,
			values:      append([]float32{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *floatColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return floatColumnIndex{&col.floatPage}, nil
}

func (col *floatColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return floatOffsetIndex{&col.floatPage}, nil
}

func (col *floatColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *floatColumnBuffer) Dictionary() Dictionary { return nil }

func (col *floatColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *floatColumnBuffer) Page() Page { return &col.floatPage }

func (col *floatColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *floatColumnBuffer) Cap() int { return cap(col.values) }

func (col *floatColumnBuffer) Len() int { return len(col.values) }

func (col *floatColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *floatColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *floatColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write FLOAT values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[float32](b)...)
	return len(b), nil
}

func (col *floatColumnBuffer) WriteFloats(values []float32) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *floatColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU32), columnLevels{})
	return len(values), nil
}

func (col *floatColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]float32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherFloat32(col.values[n:], rows.Float32Array())
}

func (col *floatColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type doubleColumnBuffer struct{ doublePage }

func newDoubleColumnBuffer(typ Type, columnIndex int16, numValues int32) *doubleColumnBuffer {
	return &doubleColumnBuffer{
		doublePage: doublePage{
			typ:         typ,
			values:      make([]float64, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *doubleColumnBuffer) Clone() ColumnBuffer {
	return &doubleColumnBuffer{
		doublePage: doublePage{
			typ:         col.typ,
			values:      append([]float64{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *doubleColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return doubleColumnIndex{&col.doublePage}, nil
}

func (col *doubleColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return doubleOffsetIndex{&col.doublePage}, nil
}

func (col *doubleColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *doubleColumnBuffer) Dictionary() Dictionary { return nil }

func (col *doubleColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *doubleColumnBuffer) Page() Page { return &col.doublePage }

func (col *doubleColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *doubleColumnBuffer) Cap() int { return cap(col.values) }

func (col *doubleColumnBuffer) Len() int { return len(col.values) }

func (col *doubleColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *doubleColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *doubleColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write DOUBLE values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[float64](b)...)
	return len(b), nil
}

func (col *doubleColumnBuffer) WriteDoubles(values []float64) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *doubleColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU64), columnLevels{})
	return len(values), nil
}

func (col *doubleColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]float64, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherFloat64(col.values[n:], rows.Float64Array())
}

func (col *doubleColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type byteArrayColumnBuffer struct {
	byteArrayPage
	lengths []uint32
	scratch []byte
}

func newByteArrayColumnBuffer(typ Type, columnIndex int16, numValues int32) *byteArrayColumnBuffer {
	return &byteArrayColumnBuffer{
		byteArrayPage: byteArrayPage{
			typ:         typ,
			values:      make([]byte, 0, typ.EstimateSize(int(numValues))),
			offsets:     make([]uint32, 0, numValues+1),
			columnIndex: ^columnIndex,
		},
		lengths: make([]uint32, 0, numValues),
	}
}

func (col *byteArrayColumnBuffer) Clone() ColumnBuffer {
	return &byteArrayColumnBuffer{
		byteArrayPage: byteArrayPage{
			typ:         col.typ,
			values:      col.cloneValues(),
			offsets:     col.cloneOffsets(),
			columnIndex: col.columnIndex,
		},
		lengths: col.cloneLengths(),
	}
}

func (col *byteArrayColumnBuffer) cloneLengths() []uint32 {
	lengths := make([]uint32, len(col.lengths))
	copy(lengths, col.lengths)
	return lengths
}

func (col *byteArrayColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return byteArrayColumnIndex{&col.byteArrayPage}, nil
}

func (col *byteArrayColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return byteArrayOffsetIndex{&col.byteArrayPage}, nil
}

func (col *byteArrayColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *byteArrayColumnBuffer) Dictionary() Dictionary { return nil }

func (col *byteArrayColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *byteArrayColumnBuffer) Page() Page {
	if len(col.lengths) > 0 && orderOfUint32(col.offsets) < 1 { // unordered?
		if cap(col.scratch) < len(col.values) {
			col.scratch = make([]byte, 0, cap(col.values))
		} else {
			col.scratch = col.scratch[:0]
		}

		for i := range col.lengths {
			n := len(col.scratch)
			col.scratch = append(col.scratch, col.index(i)...)
			col.offsets[i] = uint32(n)
		}

		col.values, col.scratch = col.scratch, col.values
	}
	// The offsets have the total length as the last item. Since we are about to
	// expose the column buffer's internal state as a Page value we ensure that
	// the last offset is the total length of all values.
	col.offsets = append(col.offsets[:len(col.lengths)], uint32(len(col.values)))
	return &col.byteArrayPage
}

func (col *byteArrayColumnBuffer) Reset() {
	col.values = col.values[:0]
	col.offsets = col.offsets[:0]
	col.lengths = col.lengths[:0]
}

func (col *byteArrayColumnBuffer) NumRows() int64 { return int64(col.Len()) }

func (col *byteArrayColumnBuffer) NumValues() int64 { return int64(col.Len()) }

func (col *byteArrayColumnBuffer) Cap() int { return cap(col.lengths) }

func (col *byteArrayColumnBuffer) Len() int { return len(col.lengths) }

func (col *byteArrayColumnBuffer) Less(i, j int) bool {
	return bytes.Compare(col.index(i), col.index(j)) < 0
}

func (col *byteArrayColumnBuffer) Swap(i, j int) {
	col.offsets[i], col.offsets[j] = col.offsets[j], col.offsets[i]
	col.lengths[i], col.lengths[j] = col.lengths[j], col.lengths[i]
}

func (col *byteArrayColumnBuffer) Write(b []byte) (int, error) {
	_, n, err := col.writeByteArrays(b)
	return n, err
}

func (col *byteArrayColumnBuffer) WriteByteArrays(values []byte) (int, error) {
	n, _, err := col.writeByteArrays(values)
	return n, err
}

func (col *byteArrayColumnBuffer) writeByteArrays(values []byte) (count, bytes int, err error) {
	baseCount := len(col.lengths)
	baseBytes := len(col.values) + (plain.ByteArrayLengthSize * len(col.lengths))

	err = plain.RangeByteArray(values, func(value []byte) error {
		col.append(unsafecast.String(value))
		return nil
	})

	count = len(col.lengths) - baseCount
	bytes = (len(col.values) - baseBytes) + (plain.ByteArrayLengthSize * count)
	return count, bytes, err
}

func (col *byteArrayColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfPtr), columnLevels{})
	return len(values), nil
}

func (col *byteArrayColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	for i := 0; i < rows.Len(); i++ {
		p := rows.Index(i)
		col.append(*(*string)(p))
	}
}

func (col *byteArrayColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.lengths)))
	case i >= len(col.lengths):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.lengths) {
			values[n] = col.makeValueBytes(col.index(i))
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

func (col *byteArrayColumnBuffer) append(value string) {
	col.offsets = append(col.offsets, uint32(len(col.values)))
	col.lengths = append(col.lengths, uint32(len(value)))
	col.values = append(col.values, value...)
}

func (col *byteArrayColumnBuffer) index(i int) []byte {
	offset := col.offsets[i]
	length := col.lengths[i]
	end := offset + length
	return col.values[offset:end:end]
}

type fixedLenByteArrayColumnBuffer struct {
	fixedLenByteArrayPage
	tmp []byte
}

func newFixedLenByteArrayColumnBuffer(typ Type, columnIndex int16, numValues int32) *fixedLenByteArrayColumnBuffer {
	size := typ.Length()
	return &fixedLenByteArrayColumnBuffer{
		fixedLenByteArrayPage: fixedLenByteArrayPage{
			typ:         typ,
			size:        size,
			data:        make([]byte, 0, typ.EstimateSize(int(numValues))),
			columnIndex: ^columnIndex,
		},
		tmp: make([]byte, size),
	}
}

func (col *fixedLenByteArrayColumnBuffer) Clone() ColumnBuffer {
	return &fixedLenByteArrayColumnBuffer{
		fixedLenByteArrayPage: fixedLenByteArrayPage{
			typ:         col.typ,
			size:        col.size,
			data:        append([]byte{}, col.data...),
			columnIndex: col.columnIndex,
		},
		tmp: make([]byte, col.size),
	}
}

func (col *fixedLenByteArrayColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return fixedLenByteArrayColumnIndex{&col.fixedLenByteArrayPage}, nil
}

func (col *fixedLenByteArrayColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return fixedLenByteArrayOffsetIndex{&col.fixedLenByteArrayPage}, nil
}

func (col *fixedLenByteArrayColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *fixedLenByteArrayColumnBuffer) Dictionary() Dictionary { return nil }

func (col *fixedLenByteArrayColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *fixedLenByteArrayColumnBuffer) Page() Page { return &col.fixedLenByteArrayPage }

func (col *fixedLenByteArrayColumnBuffer) Reset() { col.data = col.data[:0] }

func (col *fixedLenByteArrayColumnBuffer) Cap() int { return cap(col.data) / col.size }

func (col *fixedLenByteArrayColumnBuffer) Len() int { return len(col.data) / col.size }

func (col *fixedLenByteArrayColumnBuffer) Less(i, j int) bool {
	return bytes.Compare(col.index(i), col.index(j)) < 0
}

func (col *fixedLenByteArrayColumnBuffer) Swap(i, j int) {
	t, u, v := col.tmp[:col.size], col.index(i), col.index(j)
	copy(t, u)
	copy(u, v)
	copy(v, t)
}

func (col *fixedLenByteArrayColumnBuffer) index(i int) []byte {
	j := (i + 0) * col.size
	k := (i + 1) * col.size
	return col.data[j:k:k]
}

func (col *fixedLenByteArrayColumnBuffer) Write(b []byte) (int, error) {
	n, err := col.WriteFixedLenByteArrays(b)
	return n * col.size, err
}

func (col *fixedLenByteArrayColumnBuffer) WriteFixedLenByteArrays(values []byte) (int, error) {
	d, m := len(values)/col.size, len(values)%col.size
	if m != 0 {
		return 0, fmt.Errorf("cannot write FIXED_LEN_BYTE_ARRAY values of size %d from input of size %d", col.size, len(values))
	}
	col.data = append(col.data, values...)
	return d, nil
}

func (col *fixedLenByteArrayColumnBuffer) WriteValues(values []Value) (int, error) {
	for _, v := range values {
		col.data = append(col.data, v.byteArray()...)
	}
	return len(values), nil
}

func (col *fixedLenByteArrayColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	n := col.size * rows.Len()
	i := len(col.data)
	j := len(col.data) + n

	if cap(col.data) < j {
		col.data = append(make([]byte, 0, max(i+n, 2*cap(col.data))), col.data...)
	}

	col.data = col.data[:j]
	newData := col.data[i:]

	for i := 0; i < rows.Len(); i++ {
		p := rows.Index(i)
		copy(newData[i*col.size:], unsafe.Slice((*byte)(p), col.size))
	}
}

func (col *fixedLenByteArrayColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset) * col.size
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.data)/col.size))
	case i >= len(col.data):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.data) {
			values[n] = col.makeValueBytes(col.data[i : i+col.size])
			n++
			i += col.size
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type uint32ColumnBuffer struct{ uint32Page }

func newUint32ColumnBuffer(typ Type, columnIndex int16, numValues int32) *uint32ColumnBuffer {
	return &uint32ColumnBuffer{
		uint32Page: uint32Page{
			typ:         typ,
			values:      make([]uint32, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *uint32ColumnBuffer) Clone() ColumnBuffer {
	return &uint32ColumnBuffer{
		uint32Page: uint32Page{
			typ:         col.typ,
			values:      append([]uint32{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *uint32ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return uint32ColumnIndex{&col.uint32Page}, nil
}

func (col *uint32ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return uint32OffsetIndex{&col.uint32Page}, nil
}

func (col *uint32ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *uint32ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *uint32ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *uint32ColumnBuffer) Page() Page { return &col.uint32Page }

func (col *uint32ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *uint32ColumnBuffer) Cap() int { return cap(col.values) }

func (col *uint32ColumnBuffer) Len() int { return len(col.values) }

func (col *uint32ColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *uint32ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *uint32ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write INT32 values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[uint32](b)...)
	return len(b), nil
}

func (col *uint32ColumnBuffer) WriteUint32s(values []uint32) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *uint32ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU32), columnLevels{})
	return len(values), nil
}

func (col *uint32ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]uint32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherUint32(col.values[n:], rows.Uint32Array())
}

func (col *uint32ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type uint64ColumnBuffer struct{ uint64Page }

func newUint64ColumnBuffer(typ Type, columnIndex int16, numValues int32) *uint64ColumnBuffer {
	return &uint64ColumnBuffer{
		uint64Page: uint64Page{
			typ:         typ,
			values:      make([]uint64, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *uint64ColumnBuffer) Clone() ColumnBuffer {
	return &uint64ColumnBuffer{
		uint64Page: uint64Page{
			typ:         col.typ,
			values:      append([]uint64{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *uint64ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return uint64ColumnIndex{&col.uint64Page}, nil
}

func (col *uint64ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return uint64OffsetIndex{&col.uint64Page}, nil
}

func (col *uint64ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *uint64ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *uint64ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *uint64ColumnBuffer) Page() Page { return &col.uint64Page }

func (col *uint64ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *uint64ColumnBuffer) Cap() int { return cap(col.values) }

func (col *uint64ColumnBuffer) Len() int { return len(col.values) }

func (col *uint64ColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *uint64ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *uint64ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write INT64 values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[uint64](b)...)
	return len(b), nil
}

func (col *uint64ColumnBuffer) WriteUint64s(values []uint64) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *uint64ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(makeArrayValue(values, offsetOfU64), columnLevels{})
	return len(values), nil
}

func (col *uint64ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]uint64, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherUint64(col.values[n:], rows.Uint64Array())
}

func (col *uint64ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

type be128ColumnBuffer struct{ be128Page }

func newBE128ColumnBuffer(typ Type, columnIndex int16, numValues int32) *be128ColumnBuffer {
	return &be128ColumnBuffer{
		be128Page: be128Page{
			typ:         typ,
			values:      make([][16]byte, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *be128ColumnBuffer) Clone() ColumnBuffer {
	return &be128ColumnBuffer{
		be128Page: be128Page{
			typ:         col.typ,
			values:      append([][16]byte{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *be128ColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return be128ColumnIndex{&col.be128Page}, nil
}

func (col *be128ColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return be128OffsetIndex{&col.be128Page}, nil
}

func (col *be128ColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *be128ColumnBuffer) Dictionary() Dictionary { return nil }

func (col *be128ColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *be128ColumnBuffer) Page() Page { return &col.be128Page }

func (col *be128ColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *be128ColumnBuffer) Cap() int { return cap(col.values) }

func (col *be128ColumnBuffer) Len() int { return len(col.values) }

func (col *be128ColumnBuffer) Less(i, j int) bool {
	return lessBE128(&col.values[i], &col.values[j])
}

func (col *be128ColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *be128ColumnBuffer) WriteValues(values []Value) (int, error) {
	if n := len(col.values) + len(values); n > cap(col.values) {
		col.values = append(make([][16]byte, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+len(values)]
	newValues := col.values[n:]
	for i, v := range values {
		copy(newValues[i][:], v.byteArray())
	}
	return len(values), nil
}

func (col *be128ColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([][16]byte, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherUint128(col.values[n:], rows.Uint128Array())
}

func (col *be128ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.makeValue(&col.values[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

var (
	_ sort.Interface = (ColumnBuffer)(nil)
	_ io.Writer      = (*byteArrayColumnBuffer)(nil)
	_ io.Writer      = (*fixedLenByteArrayColumnBuffer)(nil)
)

// writeRowsFunc is the type of functions that apply rows to a set of column
// buffers.
//
// - columns is the array of column buffer where the rows are written.
//
// - rows is the array of Go values to write to the column buffers.
//
//   - levels is used to track the column index, repetition and definition levels
//     of values when writing optional or repeated columns.
type writeRowsFunc func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error

// writeRowsFuncOf generates a writeRowsFunc function for the given Go type and
// parquet schema. The column path indicates the column that the function is
// being generated for in the parquet schema.
func writeRowsFuncOf(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	if leaf, exists := schema.Lookup(path...); exists && leaf.Node.Type().LogicalType() != nil && leaf.Node.Type().LogicalType().Json != nil {
		return writeRowsFuncOfJSON(t, schema, path)
	}

	switch t {
	case reflect.TypeOf(deprecated.Int96{}):
		return writeRowsFuncOfRequired(t, schema, path)
	case reflect.TypeOf(time.Time{}):
		return writeRowsFuncOfTime(t, schema, path)
	}

	switch t.Kind() {
	case reflect.Bool,
		reflect.Int,
		reflect.Uint,
		reflect.Int32,
		reflect.Uint32,
		reflect.Int64,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.String:
		return writeRowsFuncOfRequired(t, schema, path)

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfRequired(t, schema, path)
		} else {
			return writeRowsFuncOfSlice(t, schema, path)
		}

	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfRequired(t, schema, path)
		}

	case reflect.Pointer:
		return writeRowsFuncOfPointer(t, schema, path)

	case reflect.Struct:
		return writeRowsFuncOfStruct(t, schema, path)

	case reflect.Map:
		return writeRowsFuncOfMap(t, schema, path)
	}

	panic("cannot convert Go values of type " + typeNameOf(t) + " to parquet value")
}

func writeRowsFuncOfRequired(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	column := schema.mapping.lookup(path)
	columnIndex := column.columnIndex
	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		columns[columnIndex].writeValues(rows, levels)
		return nil
	}
}

func writeRowsFuncOfOptional(t reflect.Type, schema *Schema, path columnPath, writeRows writeRowsFunc) writeRowsFunc {
	nullIndex := nullIndexFuncOf(t)
	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writeRows(columns, rows, levels)
		}

		nulls := acquireBitmap(rows.Len())
		defer releaseBitmap(nulls)
		nullIndex(nulls.bits, rows)

		nullLevels := levels
		levels.definitionLevel++
		// In this function, we are dealing with optional values which are
		// neither pointers nor slices; for example, a int32 field marked
		// "optional" in its parent struct.
		//
		// We need to find zero values, which should be represented as nulls
		// in the parquet column. In order to minimize the calls to writeRows
		// and maximize throughput, we use the nullIndex and nonNullIndex
		// functions, which are type-specific implementations of the algorithm.
		//
		// Sections of the input that are contiguous nulls or non-nulls can be
		// sent to a single call to writeRows to be written to the underlying
		// buffer since they share the same definition level.
		//
		// This optimization is defeated by inputs alternating null and non-null
		// sequences of single values, we do not expect this condition to be a
		// common case.
		for i := 0; i < rows.Len(); {
			j := 0
			x := i / 64
			y := i % 64

			if y != 0 {
				if b := nulls.bits[x] >> uint(y); b == 0 {
					x++
					y = 0
				} else {
					y += bits.TrailingZeros64(b)
					goto writeNulls
				}
			}

			for x < len(nulls.bits) && nulls.bits[x] == 0 {
				x++
			}

			if x < len(nulls.bits) {
				y = bits.TrailingZeros64(nulls.bits[x]) % 64
			}

		writeNulls:
			if j = x*64 + y; j > rows.Len() {
				j = rows.Len()
			}

			if i < j {
				if err := writeRows(columns, rows.Slice(i, j), nullLevels); err != nil {
					return err
				}
				i = j
			}

			if y != 0 {
				if b := nulls.bits[x] >> uint(y); b == (1<<uint64(y))-1 {
					x++
					y = 0
				} else {
					y += bits.TrailingZeros64(^b)
					goto writeNonNulls
				}
			}

			for x < len(nulls.bits) && nulls.bits[x] == ^uint64(0) {
				x++
			}

			if x < len(nulls.bits) {
				y = bits.TrailingZeros64(^nulls.bits[x]) % 64
			}

		writeNonNulls:
			if j = x*64 + y; j > rows.Len() {
				j = rows.Len()
			}

			if i < j {
				if err := writeRows(columns, rows.Slice(i, j), levels); err != nil {
					return err
				}
				i = j
			}
		}

		return nil
	}
}

func writeRowsFuncOfPointer(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	elemType := t.Elem()
	elemSize := uintptr(elemType.Size())
	writeRows := writeRowsFuncOf(elemType, schema, path)

	if len(path) == 0 {
		// This code path is taken when generating a writeRowsFunc for a pointer
		// type. In this case, we do not need to increase the definition level
		// since we are not deailng with an optional field but a pointer to the
		// row type.
		return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
			if rows.Len() == 0 {
				return writeRows(columns, rows, levels)
			}

			for i := 0; i < rows.Len(); i++ {
				p := *(*unsafe.Pointer)(rows.Index(i))
				a := sparse.Array{}
				if p != nil {
					a = makeArray(p, 1, elemSize)
				}
				if err := writeRows(columns, a, levels); err != nil {
					return err
				}
			}

			return nil
		}
	}

	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writeRows(columns, rows, levels)
		}

		for i := 0; i < rows.Len(); i++ {
			p := *(*unsafe.Pointer)(rows.Index(i))
			a := sparse.Array{}
			elemLevels := levels
			if p != nil {
				a = makeArray(p, 1, elemSize)
				elemLevels.definitionLevel++
			}
			if err := writeRows(columns, a, elemLevels); err != nil {
				return err
			}
		}

		return nil
	}
}

func writeRowsFuncOfSlice(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	elemType := t.Elem()
	elemSize := uintptr(elemType.Size())
	writeRows := writeRowsFuncOf(elemType, schema, path)

	// When the element is a pointer type, the writeRows function will be an
	// instance returned by writeRowsFuncOfPointer, which handles incrementing
	// the definition level if the pointer value is not nil.
	definitionLevelIncrement := byte(0)
	if elemType.Kind() != reflect.Ptr {
		definitionLevelIncrement = 1
	}

	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writeRows(columns, rows, levels)
		}

		levels.repetitionDepth++

		for i := 0; i < rows.Len(); i++ {
			p := (*sliceHeader)(rows.Index(i))
			a := makeArray(p.base, p.len, elemSize)
			b := sparse.Array{}

			elemLevels := levels
			if a.Len() > 0 {
				b = a.Slice(0, 1)
				elemLevels.definitionLevel += definitionLevelIncrement
			}

			if err := writeRows(columns, b, elemLevels); err != nil {
				return err
			}

			if a.Len() > 1 {
				elemLevels.repetitionLevel = elemLevels.repetitionDepth

				if err := writeRows(columns, a.Slice(1, a.Len()), elemLevels); err != nil {
					return err
				}
			}
		}

		return nil
	}
}

func writeRowsFuncOfStruct(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	type column struct {
		offset    uintptr
		writeRows writeRowsFunc
	}

	fields := structFieldsOf(t)
	columns := make([]column, len(fields))

	for i, f := range fields {
		optional := false
		columnPath := path.append(f.Name)
		forEachStructTagOption(f, func(_ reflect.Type, option, _ string) {
			switch option {
			case "list":
				columnPath = columnPath.append("list", "element")
			case "optional":
				optional = true
			}
		})

		writeRows := writeRowsFuncOf(f.Type, schema, columnPath)
		if optional {
			switch f.Type.Kind() {
			case reflect.Pointer, reflect.Slice:
			default:
				writeRows = writeRowsFuncOfOptional(f.Type, schema, columnPath, writeRows)
			}
		}

		columns[i] = column{
			offset:    f.Offset,
			writeRows: writeRows,
		}
	}

	return func(buffers []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			for _, column := range columns {
				if err := column.writeRows(buffers, rows, levels); err != nil {
					return err
				}
			}
		} else {
			for _, column := range columns {
				if err := column.writeRows(buffers, rows.Offset(column.offset), levels); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func writeRowsFuncOfMap(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	keyPath := path.append("key_value", "key")
	keyType := t.Key()
	keySize := uintptr(keyType.Size())
	writeKeys := writeRowsFuncOf(keyType, schema, keyPath)

	valuePath := path.append("key_value", "value")
	valueType := t.Elem()
	valueSize := uintptr(valueType.Size())
	writeValues := writeRowsFuncOf(valueType, schema, valuePath)

	writeKeyValues := func(columns []ColumnBuffer, keys, values sparse.Array, levels columnLevels) error {
		if err := writeKeys(columns, keys, levels); err != nil {
			return err
		}
		if err := writeValues(columns, values, levels); err != nil {
			return err
		}
		return nil
	}

	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writeKeyValues(columns, rows, rows, levels)
		}

		levels.repetitionDepth++
		mapKey := reflect.New(keyType).Elem()
		mapValue := reflect.New(valueType).Elem()

		for i := 0; i < rows.Len(); i++ {
			m := reflect.NewAt(t, rows.Index(i)).Elem()

			if m.Len() == 0 {
				empty := sparse.Array{}
				if err := writeKeyValues(columns, empty, empty, levels); err != nil {
					return err
				}
			} else {
				elemLevels := levels
				elemLevels.definitionLevel++

				for it := m.MapRange(); it.Next(); {
					mapKey.SetIterKey(it)
					mapValue.SetIterValue(it)

					k := makeArray(reflectValueData(mapKey), 1, keySize)
					v := makeArray(reflectValueData(mapValue), 1, valueSize)

					if err := writeKeyValues(columns, k, v, elemLevels); err != nil {
						return err
					}

					elemLevels.repetitionLevel = elemLevels.repetitionDepth
				}
			}
		}

		return nil
	}
}

func writeRowsFuncOfJSON(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	// If this is a string or a byte array write directly.
	switch t.Kind() {
	case reflect.String:
		return writeRowsFuncOfRequired(t, schema, path)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfRequired(t, schema, path)
		}
	}

	// Otherwise handle with a json.Marshal
	asStrT := reflect.TypeOf(string(""))
	writer := writeRowsFuncOfRequired(asStrT, schema, path)

	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writer(columns, rows, levels)
		}
		for i := 0; i < rows.Len(); i++ {
			val := reflect.NewAt(t, rows.Index(i))
			asI := val.Interface()

			b, err := json.Marshal(asI)
			if err != nil {
				return err
			}

			asStr := string(b)
			a := sparse.MakeStringArray([]string{asStr})
			if err := writer(columns, a.UnsafeArray(), levels); err != nil {
				return err
			}
		}
		return nil
	}
}

func writeRowsFuncOfTime(_ reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	t := reflect.TypeOf(int64(0))
	elemSize := uintptr(t.Size())
	writeRows := writeRowsFuncOf(t, schema, path)

	col, _ := schema.Lookup(path...)
	unit := Nanosecond.TimeUnit()
	lt := col.Node.Type().LogicalType()
	if lt != nil && lt.Timestamp != nil {
		unit = lt.Timestamp.Unit
	}

	return func(columns []ColumnBuffer, rows sparse.Array, levels columnLevels) error {
		if rows.Len() == 0 {
			return writeRows(columns, rows, levels)
		}

		times := rows.TimeArray()
		for i := 0; i < times.Len(); i++ {
			t := times.Index(i)
			var val int64
			switch {
			case unit.Millis != nil:
				val = t.UnixMilli()
			case unit.Micros != nil:
				val = t.UnixMicro()
			default:
				val = t.UnixNano()
			}

			a := makeArray(reflectValueData(reflect.ValueOf(val)), 1, elemSize)
			if err := writeRows(columns, a, levels); err != nil {
				return err
			}
		}

		return nil
	}
}
