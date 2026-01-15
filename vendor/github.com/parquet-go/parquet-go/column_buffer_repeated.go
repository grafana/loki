package parquet

import (
	"bytes"
	"io"
	"slices"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/bytealg"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

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
	repetitionLevels   memory.SliceBuffer[byte]
	definitionLevels   memory.SliceBuffer[byte]
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
	return &repeatedColumnBuffer{
		base:               base,
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		nullOrdering:       nullOrdering,
	}
}

func (col *repeatedColumnBuffer) Clone() ColumnBuffer {
	return &repeatedColumnBuffer{
		base:               col.base.Clone(),
		reordered:          col.reordered,
		maxRepetitionLevel: col.maxRepetitionLevel,
		maxDefinitionLevel: col.maxDefinitionLevel,
		rows:               slices.Clone(col.rows),
		repetitionLevels:   col.repetitionLevels.Clone(),
		definitionLevels:   col.definitionLevels.Clone(),
		nullOrdering:       col.nullOrdering,
	}
}

func (col *repeatedColumnBuffer) Type() Type {
	return col.base.Type()
}

func (col *repeatedColumnBuffer) NumValues() int64 {
	return int64(col.definitionLevels.Len())
}

func (col *repeatedColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return columnIndexOfNullable(col.base, col.maxDefinitionLevel, col.definitionLevels.Slice())
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
		repetitionLevels := col.repetitionLevels.Slice()
		definitionLevels := col.definitionLevels.Slice()

		for _, row := range col.rows {
			rowOffset := int(row.offset)
			rowLength := repeatedRowLength(repetitionLevels[rowOffset:])
			numNulls := countLevelsNotEqual(definitionLevels[rowOffset:rowOffset+rowLength], col.maxDefinitionLevel)
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
				offset:     uint32(column.repetitionLevels.Len()),
				baseOffset: uint32(baseOffset),
			})

			column.repetitionLevels.Append(repetitionLevels[rowOffset : rowOffset+rowLength]...)
			column.definitionLevels.Append(definitionLevels[rowOffset : rowOffset+rowLength]...)
			baseOffset += numValues
		}

		col.swapReorderingBuffer(column)
		col.reordered = false
	}

	return newRepeatedPage(
		col.base.Page(),
		col.maxRepetitionLevel,
		col.maxDefinitionLevel,
		col.repetitionLevels.Slice(),
		col.definitionLevels.Slice(),
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
	col.repetitionLevels.Resize(0)
	col.definitionLevels.Resize(0)
}

func (col *repeatedColumnBuffer) Size() int64 {
	return int64(8*len(col.rows)+col.repetitionLevels.Len()+col.definitionLevels.Len()) + col.base.Size()
}

func (col *repeatedColumnBuffer) Cap() int { return cap(col.rows) }

func (col *repeatedColumnBuffer) Len() int { return len(col.rows) }

func (col *repeatedColumnBuffer) Less(i, j int) bool {
	row1 := col.rows[i]
	row2 := col.rows[j]
	less := col.nullOrdering
	repetitionLevels := col.repetitionLevels.Slice()
	definitionLevels := col.definitionLevels.Slice()
	row1Length := repeatedRowLength(repetitionLevels[row1.offset:])
	row2Length := repeatedRowLength(repetitionLevels[row2.offset:])

	for k := 0; k < row1Length && k < row2Length; k++ {
		x := int(row1.baseOffset)
		y := int(row2.baseOffset)
		definitionLevel1 := definitionLevels[int(row1.offset)+k]
		definitionLevel2 := definitionLevels[int(row2.offset)+k]
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
			offset:     uint32(col.repetitionLevels.Len()),
			baseOffset: uint32(baseOffset),
		})
	}

	for _, v := range row {
		col.repetitionLevels.AppendValue(v.repetitionLevel)
		col.definitionLevels.AppendValue(v.definitionLevel)
	}

	return nil
}

func (col *repeatedColumnBuffer) writeValues(levels columnLevels, row sparse.Array) {
	if levels.repetitionLevel == 0 {
		col.rows = append(col.rows, offsetMapping{
			offset:     uint32(col.repetitionLevels.Len()),
			baseOffset: uint32(col.base.NumValues()),
		})
	}

	if row.Len() == 0 {
		col.repetitionLevels.AppendValue(levels.repetitionLevel)
		col.definitionLevels.AppendValue(levels.definitionLevel)
		return
	}

	// Append multiple copies of the level values
	count := row.Len()
	repStart := col.repetitionLevels.Len()
	defStart := col.definitionLevels.Len()
	col.repetitionLevels.Resize(repStart + count)
	col.definitionLevels.Resize(defStart + count)
	bytealg.Broadcast(col.repetitionLevels.Slice()[repStart:], levels.repetitionLevel)
	bytealg.Broadcast(col.definitionLevels.Slice()[defStart:], levels.definitionLevel)

	if levels.definitionLevel == col.maxDefinitionLevel {
		col.base.writeValues(levels, row)
	}
}

func (col *repeatedColumnBuffer) writeLevel(levels columnLevels) bool {
	if levels.repetitionLevel == 0 {
		col.rows = append(col.rows, offsetMapping{
			offset:     uint32(col.repetitionLevels.Len()),
			baseOffset: uint32(col.base.NumValues()),
		})
	}
	col.repetitionLevels.AppendValue(levels.repetitionLevel)
	col.definitionLevels.AppendValue(levels.definitionLevel)
	return levels.definitionLevel == col.maxDefinitionLevel
}

func (col *repeatedColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	if col.writeLevel(levels) {
		col.base.writeBoolean(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeInt32(levels columnLevels, value int32) {
	if col.writeLevel(levels) {
		col.base.writeInt32(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeInt64(levels columnLevels, value int64) {
	if col.writeLevel(levels) {
		col.base.writeInt64(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	if col.writeLevel(levels) {
		col.base.writeInt96(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeFloat(levels columnLevels, value float32) {
	if col.writeLevel(levels) {
		col.base.writeFloat(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeDouble(levels columnLevels, value float64) {
	if col.writeLevel(levels) {
		col.base.writeDouble(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	if col.writeLevel(levels) {
		col.base.writeByteArray(levels, value)
	}
}

func (col *repeatedColumnBuffer) writeNull(levels columnLevels) {
	col.writeLevel(levels)
}

func (col *repeatedColumnBuffer) ReadValuesAt(values []Value, offset int64) (int, error) {
	length := int64(col.definitionLevels.Len())
	if offset < 0 {
		return 0, errRowIndexOutOfBounds(offset, length)
	}
	if offset >= length {
		return 0, io.EOF
	}
	if length -= offset; length < int64(len(values)) {
		values = values[:length]
	}

	definitionLevelsSlice := col.definitionLevels.Slice()
	repetitionLevelsSlice := col.repetitionLevels.Slice()

	numNulls1 := int64(countLevelsNotEqual(definitionLevelsSlice[:offset], col.maxDefinitionLevel))
	numNulls2 := int64(countLevelsNotEqual(definitionLevelsSlice[offset:offset+length], col.maxDefinitionLevel))

	if numNulls2 < length {
		n, err := col.base.ReadValuesAt(values[:length-numNulls2], offset-numNulls1)
		if err != nil {
			return n, err
		}
	}

	definitionLevels := definitionLevelsSlice[offset : offset+length]
	repetitionLevels := repetitionLevelsSlice[offset : offset+length]

	if numNulls2 > 0 {
		columnIndex := ^int16(col.Column())
		i := length - numNulls2 - 1 // Last index of non-null values
		j := length - 1             // Last index in output values array
		maxDefinitionLevel := col.maxDefinitionLevel

		for n := len(definitionLevels) - 1; n >= 0 && j > i; n-- {
			if definitionLevels[n] != maxDefinitionLevel {
				values[j] = Value{
					repetitionLevel: repetitionLevels[n],
					definitionLevel: definitionLevels[n],
					columnIndex:     columnIndex,
				}
			} else {
				values[j] = values[i]
				values[j].repetitionLevel = repetitionLevels[n]
				values[j].definitionLevel = maxDefinitionLevel
				i--
			}
			j--
		}

		// Set levels on remaining non-null values at the beginning
		for k := int64(0); k <= i; k++ {
			values[k].repetitionLevel = repetitionLevels[k]
			values[k].definitionLevel = maxDefinitionLevel
		}
	} else {
		// No nulls, but still need to set levels on all values
		for i := range values[:length] {
			values[i].repetitionLevel = repetitionLevels[i]
			values[i].definitionLevel = col.maxDefinitionLevel
		}
	}

	return int(length), nil
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
