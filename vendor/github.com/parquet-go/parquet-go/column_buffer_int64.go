package parquet

import (
	"fmt"
	"io"
	"strconv"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type int64ColumnBuffer struct{ int64Page }

func newInt64ColumnBuffer(typ Type, columnIndex int16, numValues int32) *int64ColumnBuffer {
	return &int64ColumnBuffer{
		int64Page: int64Page{
			typ:         typ,
			values:      memory.SliceBufferFor[int64](int(numValues)),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *int64ColumnBuffer) Clone() ColumnBuffer {
	cloned := &int64ColumnBuffer{
		int64Page: int64Page{
			typ:         col.typ,
			columnIndex: col.columnIndex,
		},
	}
	cloned.values.Append(col.values.Slice()...)
	return cloned
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

func (col *int64ColumnBuffer) Reset() { col.values.Reset() }

func (col *int64ColumnBuffer) Cap() int { return col.values.Cap() }

func (col *int64ColumnBuffer) Len() int { return col.values.Len() }

func (col *int64ColumnBuffer) Less(i, j int) bool { return col.values.Less(i, j) }

func (col *int64ColumnBuffer) Swap(i, j int) { col.values.Swap(i, j) }

func (col *int64ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write INT64 values from input of size %d", len(b))
	}
	col.values.Append(unsafecast.Slice[int64](b)...)
	return len(b), nil
}

func (col *int64ColumnBuffer) WriteInt64s(values []int64) (int, error) {
	col.values.Append(values...)
	return len(values), nil
}

func (col *int64ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU64))
	return len(values), nil
}

func (col *int64ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	offset := col.values.Len()
	col.values.Resize(offset + rows.Len())
	sparse.GatherInt64(col.values.Slice()[offset:], rows.Int64Array())
}

func (col *int64ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var intValue int64
	if value {
		intValue = 1
	}
	col.values.AppendValue(intValue)
}

func (col *int64ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values.AppendValue(int64(value))
}

func (col *int64ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values.AppendValue(value)
}

func (col *int64ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values.AppendValue(value.Int64())
}

func (col *int64ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values.AppendValue(int64(value))
}

func (col *int64ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values.AppendValue(int64(value))
}

func (col *int64ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	intValue, err := strconv.ParseInt(unsafecast.String(value), 10, 64)
	if err != nil {
		panic("cannot write byte array to int64 column: " + err.Error())
	}
	col.values.AppendValue(intValue)
}

func (col *int64ColumnBuffer) writeNull(levels columnLevels) {
	col.values.AppendValue(0)
}

func (col *int64ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	colValues := col.values.Slice()
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(colValues)))
	case i >= len(colValues):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(colValues) {
			values[n] = col.makeValue(colValues[i])
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}
