package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/sparse"
)

type nullColumnBuffer struct{ nullPage }

func newNullColumnBuffer(typ Type, columnIndex uint16, numValues int32) *nullColumnBuffer {
	return &nullColumnBuffer{
		nullPage: nullPage{
			typ:    typ,
			column: int(columnIndex),
			count:  int(numValues),
		},
	}
}

func (col *nullColumnBuffer) Clone() ColumnBuffer {
	cloned := &nullColumnBuffer{
		nullPage: nullPage{
			typ:    col.typ,
			column: col.column,
			count:  col.count,
		},
	}
	return cloned
}

func (col *nullColumnBuffer) ColumnIndex() (ColumnIndex, error) { return nil, nil }

func (col *nullColumnBuffer) OffsetIndex() (OffsetIndex, error) { return nil, nil }

func (col *nullColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *nullColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *nullColumnBuffer) Page() Page { return &col.nullPage }

func (col *nullColumnBuffer) Reset() { col.nullPage.count = 0 }

func (col *nullColumnBuffer) Cap() int { return col.nullPage.count }

func (col *nullColumnBuffer) Len() int { return col.nullPage.count }

func (col *nullColumnBuffer) Less(i, j int) bool { return false }

func (col *nullColumnBuffer) Swap(i, j int) {}

func (col *nullColumnBuffer) Size() int64 { return 0 }

func (col *nullColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, 0))
	return len(values), nil
}

func (col *nullColumnBuffer) writeValues(_ columnLevels, rows sparse.Array) {
	col.nullPage.count += rows.Len()
}

func (col *nullColumnBuffer) writeBoolean(_ columnLevels, _ bool) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeInt32(_ columnLevels, _ int32) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeInt64(_ columnLevels, _ int64) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeInt96(_ columnLevels, _ deprecated.Int96) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeFloat(_ columnLevels, _ float32) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeDouble(_ columnLevels, _ float64) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeByteArray(_ columnLevels, _ []byte) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) writeNull(_ columnLevels) {
	col.nullPage.count++
}

func (col *nullColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(col.nullPage.count))
	case i >= int(col.nullPage.count):
		return 0, io.EOF
	default:
		for n < len(values) && i < int(col.nullPage.count) {
			values[n] = Value{columnIndex: ^uint16(col.nullPage.column)}
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}
