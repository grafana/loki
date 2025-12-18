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
			values:      slices.Clone(col.values),
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
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU32))
	return len(values), nil
}

func (col *uint32ColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]uint32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherUint32(col.values[n:], rows.Uint32Array())
}

func (col *uint32ColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var uintValue uint32
	if value {
		uintValue = 1
	}
	col.values = append(col.values, uintValue)
}

func (col *uint32ColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values = append(col.values, uint32(value))
}

func (col *uint32ColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values = append(col.values, uint32(value))
}

func (col *uint32ColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values = append(col.values, uint32(value.Int32()))
}

func (col *uint32ColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values = append(col.values, uint32(value))
}

func (col *uint32ColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values = append(col.values, uint32(value))
}

func (col *uint32ColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	uintValue, err := strconv.ParseUint(unsafecast.String(value), 10, 32)
	if err != nil {
		panic("cannot write byte array to uint32 column: " + err.Error())
	}
	col.values = append(col.values, uint32(uintValue))
}

func (col *uint32ColumnBuffer) writeNull(levels columnLevels) {
	col.values = append(col.values, 0)
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
