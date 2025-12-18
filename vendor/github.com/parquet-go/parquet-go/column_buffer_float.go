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

type floatColumnBuffer struct{ floatPage }

func newFloatColumnBuffer(typ Type, columnIndex int16, numValues int32) *floatColumnBuffer {
	return &floatColumnBuffer{
		floatPage: floatPage{
			typ:         typ,
			values:      make([]float32, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *floatColumnBuffer) Clone() ColumnBuffer {
	return &floatColumnBuffer{
		floatPage: floatPage{
			typ:         col.typ,
			values:      slices.Clone(col.values),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *floatColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return floatColumnIndex{&col.floatPage}, nil
}

func (col *floatColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return floatOffsetIndex{&col.floatPage}, nil
}

func (col *floatColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *floatColumnBuffer) Dictionary() Dictionary { return nil }

func (col *floatColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *floatColumnBuffer) Page() Page { return &col.floatPage }

func (col *floatColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *floatColumnBuffer) Cap() int { return cap(col.values) }

func (col *floatColumnBuffer) Len() int { return len(col.values) }

func (col *floatColumnBuffer) Less(i, j int) bool { return col.values[i] < col.values[j] }

func (col *floatColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *floatColumnBuffer) Write(b []byte) (int, error) {
	if (len(b) % 4) != 0 {
		return 0, fmt.Errorf("cannot write FLOAT values from input of size %d", len(b))
	}
	col.values = append(col.values, unsafecast.Slice[float32](b)...)
	return len(b), nil
}

func (col *floatColumnBuffer) WriteFloats(values []float32) (int, error) {
	col.values = append(col.values, values...)
	return len(values), nil
}

func (col *floatColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfU32))
	return len(values), nil
}

func (col *floatColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	if n := len(col.values) + rows.Len(); n > cap(col.values) {
		col.values = append(make([]float32, 0, max(n, 2*cap(col.values))), col.values...)
	}
	n := len(col.values)
	col.values = col.values[:n+rows.Len()]
	sparse.GatherFloat32(col.values[n:], rows.Float32Array())
}

func (col *floatColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var floatValue float32
	if value {
		floatValue = 1
	}
	col.values = append(col.values, floatValue)
}

func (col *floatColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.values = append(col.values, float32(value))
}

func (col *floatColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.values = append(col.values, float32(value))
}

func (col *floatColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	floatValue, _ := value.Int().Float64()
	col.values = append(col.values, float32(floatValue))
}

func (col *floatColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.values = append(col.values, value)
}

func (col *floatColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.values = append(col.values, float32(value))
}

func (col *floatColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	floatValue, err := strconv.ParseFloat(unsafecast.String(value), 32)
	if err != nil {
		panic("cannot write byte array to float column: " + err.Error())
	}
	col.values = append(col.values, float32(floatValue))
}

func (col *floatColumnBuffer) writeNull(levels columnLevels) {
	col.values = append(col.values, 0)
}

func (col *floatColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
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
