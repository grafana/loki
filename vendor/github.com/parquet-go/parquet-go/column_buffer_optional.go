package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

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
	rows               memory.SliceBuffer[int32]
	sortIndex          memory.SliceBuffer[int32]
	definitionLevels   memory.SliceBuffer[byte]
	nullOrdering       nullOrdering
}

func newOptionalColumnBuffer(base ColumnBuffer, maxDefinitionLevel byte, nullOrdering nullOrdering) *optionalColumnBuffer {
	return &optionalColumnBuffer{
		base:               base,
		maxDefinitionLevel: maxDefinitionLevel,
		nullOrdering:       nullOrdering,
	}
}

func (col *optionalColumnBuffer) Clone() ColumnBuffer {
	return &optionalColumnBuffer{
		base:               col.base.Clone(),
		reordered:          col.reordered,
		maxDefinitionLevel: col.maxDefinitionLevel,
		rows:               col.rows.Clone(),
		definitionLevels:   col.definitionLevels.Clone(),
		nullOrdering:       col.nullOrdering,
	}
}

func (col *optionalColumnBuffer) Type() Type {
	return col.base.Type()
}

func (col *optionalColumnBuffer) NumValues() int64 {
	return int64(col.definitionLevels.Len())
}

func (col *optionalColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return columnIndexOfNullable(col.base, col.maxDefinitionLevel, col.definitionLevels.Slice())
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
		numNulls := countLevelsNotEqual(col.definitionLevels.Slice(), col.maxDefinitionLevel)
		numValues := col.rows.Len() - numNulls

		if numValues > 0 {
			if col.sortIndex.Cap() < numValues {
				col.sortIndex = memory.SliceBufferFor[int32](numValues)
			}
			col.sortIndex.Resize(numValues)
			sortIndex := col.sortIndex.Slice()
			rows := col.rows.Slice()
			i := 0
			for _, j := range rows {
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

		rows := col.rows.Slice()
		i := 0
		for _, r := range rows {
			if r >= 0 {
				rows[i] = int32(i)
				i++
			}
		}

		col.reordered = false
	}

	return newOptionalPage(col.base.Page(), col.maxDefinitionLevel, col.definitionLevels.Slice())
}

func (col *optionalColumnBuffer) Reset() {
	col.base.Reset()
	col.rows.Resize(0)
	col.definitionLevels.Resize(0)
}

func (col *optionalColumnBuffer) Size() int64 {
	return int64(4*col.rows.Len()+4*col.sortIndex.Len()+col.definitionLevels.Len()) + col.base.Size()
}

func (col *optionalColumnBuffer) Cap() int { return col.rows.Cap() }

func (col *optionalColumnBuffer) Len() int { return col.rows.Len() }

func (col *optionalColumnBuffer) Less(i, j int) bool {
	rows := col.rows.Slice()
	definitionLevels := col.definitionLevels.Slice()
	return col.nullOrdering(
		col.base,
		int(rows[i]),
		int(rows[j]),
		col.maxDefinitionLevel,
		definitionLevels[i],
		definitionLevels[j],
	)
}

func (col *optionalColumnBuffer) Swap(i, j int) {
	// Because the underlying column does not contain null values, we cannot
	// swap its values at indexes i and j. We swap the row indexes only, then
	// reorder the underlying buffer using a cyclic sort when the buffer is
	// materialized into a page view.
	col.reordered = true
	rows := col.rows.Slice()
	definitionLevels := col.definitionLevels.Slice()
	rows[i], rows[j] = rows[j], rows[i]
	definitionLevels[i], definitionLevels[j] = definitionLevels[j], definitionLevels[i]
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
			col.rows.AppendValue(-1)
			col.definitionLevels.AppendValue(v.definitionLevel)
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

			for range count {
				col.definitionLevels.AppendValue(col.maxDefinitionLevel)
			}

			for count > 0 {
				col.rows.AppendValue(rowIndex)
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

func (col *optionalColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	// The row count is zero when writing an null optional value, in which case
	// we still need to output a row to the buffer to record the definition
	// level.
	if rows.Len() == 0 {
		col.definitionLevels.AppendValue(levels.definitionLevel)
		col.rows.AppendValue(-1)
		return
	}

	baseLen := col.base.Len()
	for range rows.Len() {
		col.definitionLevels.AppendValue(levels.definitionLevel)
	}

	i := col.rows.Len()
	j := col.rows.Len() + rows.Len()

	if j <= col.rows.Cap() {
		col.rows.Resize(j)
	} else {
		col.rows.Grow(j - col.rows.Len())
		col.rows.Resize(j)
	}

	rowsSlice := col.rows.Slice()
	if levels.definitionLevel != col.maxDefinitionLevel {
		broadcastValueInt32(rowsSlice[i:], -1)
	} else {
		broadcastRangeInt32(rowsSlice[i:], int32(baseLen))
		col.base.writeValues(levels, rows)
	}
}

func (col *optionalColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeBoolean(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeInt32(levels columnLevels, value int32) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeInt32(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeInt64(levels columnLevels, value int64) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeInt64(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeInt96(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeFloat(levels columnLevels, value float32) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeFloat(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeDouble(levels columnLevels, value float64) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeDouble(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	if levels.definitionLevel != col.maxDefinitionLevel {
		col.writeNull(levels)
	} else {
		col.base.writeByteArray(levels, value)
		col.writeLevel()
	}
}

func (col *optionalColumnBuffer) writeNull(levels columnLevels) {
	col.definitionLevels.AppendValue(levels.definitionLevel)
	col.rows.AppendValue(-1)
}

func (col *optionalColumnBuffer) writeLevel() {
	col.definitionLevels.AppendValue(col.maxDefinitionLevel)
	col.rows.AppendValue(int32(col.base.Len() - 1))
}

func (col *optionalColumnBuffer) ReadValuesAt(values []Value, offset int64) (int, error) {
	definitionLevels := col.definitionLevels.Slice()
	length := int64(len(definitionLevels))
	if offset < 0 {
		return 0, errRowIndexOutOfBounds(offset, length)
	}
	if offset >= length {
		return 0, io.EOF
	}
	if length -= offset; length < int64(len(values)) {
		values = values[:length]
	}

	numNulls1 := int64(countLevelsNotEqual(definitionLevels[:offset], col.maxDefinitionLevel))
	numNulls2 := int64(countLevelsNotEqual(definitionLevels[offset:offset+length], col.maxDefinitionLevel))

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
		definitionLevelsSlice := definitionLevels[offset : offset+length]
		maxDefinitionLevel := col.maxDefinitionLevel

		for n := len(definitionLevelsSlice) - 1; n >= 0 && j > i; n-- {
			if definitionLevelsSlice[n] != maxDefinitionLevel {
				values[j] = Value{definitionLevel: definitionLevelsSlice[n], columnIndex: columnIndex}
			} else {
				values[j] = values[i]
				i--
			}
			j--
		}
	}

	return int(length), nil
}
