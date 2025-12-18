package parquet

import (
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/sparse"
)

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
			values:      slices.Clone(col.values),
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
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU32))
	return len(values), nil
}

func (col *int32ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]int32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherInt32(col.values[n:], rows.Int32Array())

}

func (col *int32ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var intValue int32
	if value {
		intValue = 1
	}
	col.values = append(col.values, intValue)
}

func (col *int32ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values = append(col.values, value)
}

func (col *int32ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values = append(col.values, int32(value))
}

func (col *int32ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values = append(col.values, value.Int32())
}

func (col *int32ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values = append(col.values, int32(value))
}

func (col *int32ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values = append(col.values, int32(value))
}

func (col *int32ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	intValue, err := strconv.ParseInt(unsafecast.String(value), 10, 32)
	if err != nil {
		panic("cannot write byte array to int32 column: " + err.Error())
	}
	col.values = append(col.values, int32(intValue))
}

func (col *int32ColumnBuffer) writeNull(levels columnLevels) {
	col.values = append(col.values, 0)
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
