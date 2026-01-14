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

type int32ColumnBuffer struct{ int32Page }

func newInt32ColumnBuffer(typ Type, columnIndex int16, numValues int32) *int32ColumnBuffer {
	return &int32ColumnBuffer{
		int32Page: int32Page{
			typ:         typ,
			values:      memory.SliceBufferFor[int32](int(numValues)),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *int32ColumnBuffer) Clone() ColumnBuffer {
	return &int32ColumnBuffer{
		int32Page: int32Page{
			typ:         col.typ,
			values:      col.values.Clone(),
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

func (col *int32ColumnBuffer) Reset() { col.values.Reset() }

func (col *int32ColumnBuffer) Cap() int { return col.values.Cap() }

func (col *int32ColumnBuffer) Len() int { return col.values.Len() }

func (col *int32ColumnBuffer) Less(i, j int) bool { return col.values.Less(i, j) }

func (col *int32ColumnBuffer) Swap(i, j int) { col.values.Swap(i, j) }

func (col *int32ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write INT32 values from input of size %d", len(b))
	}
	col.values.Append(unsafecast.Slice[int32](b)...)
	return len(b), nil
}

func (col *int32ColumnBuffer) WriteInt32s(values []int32) (int, error) {
	col.values.Append(values...)
	return len(values), nil
}

func (col *int32ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU32))
	return len(values), nil
}

func (col *int32ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	offset := col.values.Len()
	col.values.Resize(offset + rows.Len())
	sparse.GatherInt32(col.values.Slice()[offset:], rows.Int32Array())
}

func (col *int32ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var intValue int32
	if value {
		intValue = 1
	}
	col.values.AppendValue(intValue)
}

func (col *int32ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values.AppendValue(value)
}

func (col *int32ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values.AppendValue(int32(value))
}

func (col *int32ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values.AppendValue(value.Int32())
}

func (col *int32ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values.AppendValue(int32(value))
}

func (col *int32ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values.AppendValue(int32(value))
}

func (col *int32ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	intValue, err := strconv.ParseInt(unsafecast.String(value), 10, 32)
	if err != nil {
		panic("cannot write byte array to int32 column: " + err.Error())
	}
	col.values.AppendValue(int32(intValue))
}

func (col *int32ColumnBuffer) writeNull(levels columnLevels) {
	col.values.AppendValue(0)
}

func (col *int32ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
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
