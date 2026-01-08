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

type uint64ColumnBuffer struct{ uint64Page }

func newUint64ColumnBuffer(typ Type, columnIndex int16, numValues int32) *uint64ColumnBuffer {
	return &uint64ColumnBuffer{
		uint64Page: uint64Page{
			typ:         typ,
			values:      memory.SliceBufferFor[uint64](int(numValues)),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *uint64ColumnBuffer) Clone() ColumnBuffer {
	cloned := &uint64ColumnBuffer{
		uint64Page: uint64Page{
			typ:         col.typ,
			columnIndex: col.columnIndex,
		},
	}
	cloned.values.Append(col.values.Slice()...)
	return cloned
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

func (col *uint64ColumnBuffer) Reset() { col.values.Reset() }

func (col *uint64ColumnBuffer) Cap() int { return col.values.Cap() }

func (col *uint64ColumnBuffer) Len() int { return col.values.Len() }

func (col *uint64ColumnBuffer) Less(i, j int) bool { return col.values.Less(i, j) }

func (col *uint64ColumnBuffer) Swap(i, j int) { col.values.Swap(i, j) }

func (col *uint64ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write INT64 values from input of size %d", len(b))
	}
	col.values.Append(unsafecast.Slice[uint64](b)...)
	return len(b), nil
}

func (col *uint64ColumnBuffer) WriteUint64s(values []uint64) (int, error) {
	col.values.Append(values...)
	return len(values), nil
}

func (col *uint64ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU64))
	return len(values), nil
}

func (col *uint64ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	offset := col.values.Len()
	col.values.Resize(offset + rows.Len())
	sparse.GatherUint64(col.values.Slice()[offset:], rows.Uint64Array())
}

func (col *uint64ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var uintValue uint64
	if value {
		uintValue = 1
	}
	col.values.AppendValue(uintValue)
}

func (col *uint64ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values.AppendValue(uint64(value))
}

func (col *uint64ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values.AppendValue(uint64(value))
}

func (col *uint64ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values.AppendValue(uint64(value.Int32()))
}

func (col *uint64ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values.AppendValue(uint64(value))
}

func (col *uint64ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values.AppendValue(uint64(value))
}

func (col *uint64ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	uintValue, err := strconv.ParseUint(unsafecast.String(value), 10, 32)
	if err != nil {
		panic("cannot write byte array to uint64 column: " + err.Error())
	}
	col.values.AppendValue(uint64(uintValue))
}

func (col *uint64ColumnBuffer) writeNull(levels columnLevels) {
	col.values.AppendValue(0)
}

func (col *uint64ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
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
