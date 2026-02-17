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

type uint32ColumnBuffer struct{ uint32Page }

func newUint32ColumnBuffer(typ Type, columnIndex int16, numValues int32) *uint32ColumnBuffer {
	return &uint32ColumnBuffer{
		uint32Page: uint32Page{
			typ:         typ,
			values:      memory.SliceBufferFor[uint32](int(numValues)),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *uint32ColumnBuffer) Clone() ColumnBuffer {
	cloned := &uint32ColumnBuffer{
		uint32Page: uint32Page{
			typ:         col.typ,
			columnIndex: col.columnIndex,
		},
	}
	cloned.values.Append(col.values.Slice()...)
	return cloned
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

func (col *uint32ColumnBuffer) Reset() { col.values.Reset() }

func (col *uint32ColumnBuffer) Cap() int { return col.values.Cap() }

func (col *uint32ColumnBuffer) Len() int { return col.values.Len() }

func (col *uint32ColumnBuffer) Less(i, j int) bool { return col.values.Less(i, j) }

func (col *uint32ColumnBuffer) Swap(i, j int) { col.values.Swap(i, j) }

func (col *uint32ColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write INT32 values from input of size %d", len(b))
	}
	col.values.Append(unsafecast.Slice[uint32](b)...)
	return len(b), nil
}

func (col *uint32ColumnBuffer) WriteUint32s(values []uint32) (int, error) {
	col.values.Append(values...)
	return len(values), nil
}

func (col *uint32ColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU32))
	return len(values), nil
}

func (col *uint32ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	offset := col.values.Len()
	col.values.Resize(offset + rows.Len())
	sparse.GatherUint32(col.values.Slice()[offset:], rows.Uint32Array())
}

func (col *uint32ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var uintValue uint32
	if value {
		uintValue = 1
	}
	col.values.AppendValue(uintValue)
}

func (col *uint32ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values.AppendValue(uint32(value))
}

func (col *uint32ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values.AppendValue(uint32(value))
}

func (col *uint32ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values.AppendValue(uint32(value.Int32()))
}

func (col *uint32ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values.AppendValue(uint32(value))
}

func (col *uint32ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values.AppendValue(uint32(value))
}

func (col *uint32ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	uintValue, err := strconv.ParseUint(unsafecast.String(value), 10, 32)
	if err != nil {
		panic("cannot write byte array to uint32 column: " + err.Error())
	}
	col.values.AppendValue(uint32(uintValue))
}

func (col *uint32ColumnBuffer) writeNull(levels columnLevels) {
	col.values.AppendValue(0)
}

func (col *uint32ColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
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
