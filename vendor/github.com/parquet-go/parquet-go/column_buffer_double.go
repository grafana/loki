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

type doubleColumnBuffer struct{ doublePage }

func newDoubleColumnBuffer(typ Type, columnIndex int16, numValues int32) *doubleColumnBuffer {
	return &doubleColumnBuffer{
		doublePage: doublePage{
			typ:         typ,
			values:      memory.SliceBufferFor[float64](int(numValues)),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *doubleColumnBuffer) Clone() ColumnBuffer {
	return &doubleColumnBuffer{
		doublePage: doublePage{
			typ:         col.typ,
			values:      col.values.Clone(),
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

func (col *doubleColumnBuffer) Reset() { col.values.Reset() }

func (col *doubleColumnBuffer) Cap() int { return col.values.Cap() }

func (col *doubleColumnBuffer) Len() int { return col.values.Len() }

func (col *doubleColumnBuffer) Less(i, j int) bool { return col.values.Less(i, j) }

func (col *doubleColumnBuffer) Swap(i, j int) { col.values.Swap(i, j) }

func (col *doubleColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 8) != 0 {
		return 0, fmt.Errorf("cannot write DOUBLE values from input of size %d", len(b))
	}
	col.values.Append(unsafecast.Slice[float64](b)...)
	return len(b), nil
}

func (col *doubleColumnBuffer) WriteDoubles(values []float64) (int, error) {
	col.values.Append(values...)
	return len(values), nil
}

func (col *doubleColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU64))
	return len(values), nil
}

func (col *doubleColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	offset := col.values.Len()
	col.values.Resize(offset + rows.Len())
	sparse.GatherFloat64(col.values.Slice()[offset:], rows.Float64Array())
}

func (col *doubleColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var uintValue float64
	if value {
		uintValue = 1
	}
	col.values.AppendValue(uintValue)
}

func (col *doubleColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values.AppendValue(float64(value))
}

func (col *doubleColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values.AppendValue(float64(value))
}

func (col *doubleColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.values.AppendValue(float64(value.Int32()))
}

func (col *doubleColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values.AppendValue(float64(value))
}

func (col *doubleColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values.AppendValue(float64(value))
}

func (col *doubleColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	floatValue, err := strconv.ParseFloat(unsafecast.String(value), 64)
	if err != nil {
		panic("cannot write byte array to double column: " + err.Error())
	}
	col.values.AppendValue(floatValue)
}

func (col *doubleColumnBuffer) writeNull(levels columnLevels) {
	col.values.AppendValue(0)
}

func (col *doubleColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
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
