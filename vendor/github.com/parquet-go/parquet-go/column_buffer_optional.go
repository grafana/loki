package parquet

import (
	"io"
	"slices"

	"github.com/parquet-go/parquet-go/deprecated"
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
	rows               []int32
	sortIndex          []int32
	definitionLevels   []byte
	nullOrdering       nullOrdering
}

func newOptionalColumnBuffer(base ColumnBuffer, rows []int32, levels []byte, maxDefinitionLevel byte, nullOrdering nullOrdering) *optionalColumnBuffer {
	return &optionalColumnBuffer{
		base:               base,
		rows:               rows,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   levels,
		nullOrdering:       nullOrdering,
	}
}

func (col *optionalColumnBuffer) Clone() ColumnBuffer {
	return &optionalColumnBuffer{
		base:               col.base.Clone(),
		reordered:          col.reordered,
		maxDefinitionLevel: col.maxDefinitionLevel,
		rows:               slices.Clone(col.rows),
		definitionLevels:   slices.Clone(col.definitionLevels),
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

func (col *optionalColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	// The row count is zero when writing an null optional value, in which case
	// we still need to output a row to the buffer to record the definition
	// level.
	if rows.Len() == 0 {
		col.definitionLevels = append(col.definitionLevels, levels.definitionLevel)
		col.rows = append(col.rows, -1)
		return
	}

	baseLen := col.base.Len()
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
		broadcastRangeInt32(col.rows[i:], int32(baseLen))
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
	col.definitionLevels = append(col.definitionLevels, levels.definitionLevel)
	col.rows = append(col.rows, -1)
}

func (col *optionalColumnBuffer) writeLevel() {
	col.definitionLevels = append(col.definitionLevels, col.maxDefinitionLevel)
	col.rows = append(col.rows, int32(col.base.Len()-1))
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
