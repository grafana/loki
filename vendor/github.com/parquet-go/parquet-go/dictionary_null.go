package parquet

import (
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/sparse"
)

// nullDictionary is a dictionary for NULL type columns where all operations are no-ops.
type nullDictionary struct {
	nullPage
}

func newNullDictionary(typ Type, columnIndex int16, numValues int32, _ encoding.Values) *nullDictionary {
	return &nullDictionary{
		nullPage: *newNullPage(typ, columnIndex, numValues),
	}
}

func (d *nullDictionary) Type() Type { return d.nullPage.Type() }

func (d *nullDictionary) Len() int { return int(d.nullPage.count) }

func (d *nullDictionary) Size() int64 { return 0 }

func (d *nullDictionary) Index(i int32) Value { return NullValue() }

func (d *nullDictionary) Lookup(indexes []int32, values []Value) {
	checkLookupIndexBounds(indexes, makeArrayValue(values, 0))
	for i := range indexes {
		values[i] = NullValue()
	}
}

func (d *nullDictionary) Insert(indexes []int32, values []Value) {}

func (d *nullDictionary) Bounds(indexes []int32) (min, max Value) {
	return NullValue(), NullValue()
}

func (d *nullDictionary) Reset() {
	d.nullPage.count = 0
}

func (d *nullDictionary) Page() Page { return &d.nullPage }

func (d *nullDictionary) insert(indexes []int32, rows sparse.Array) {}

func (d *nullDictionary) insertBoolean(value bool) int32 {
	panic("cannot insert boolean value into null dictionary")
}

func (d *nullDictionary) insertInt32(value int32) int32 {
	panic("cannot insert int32 value into null dictionary")
}

func (d *nullDictionary) insertInt64(value int64) int32 {
	panic("cannot insert int64 value into null dictionary")
}

func (d *nullDictionary) insertInt96(value deprecated.Int96) int32 {
	panic("cannot insert int96 value into null dictionary")
}

func (d *nullDictionary) insertFloat(value float32) int32 {
	panic("cannot insert float value into null dictionary")
}

func (d *nullDictionary) insertDouble(value float64) int32 {
	panic("cannot insert double value into null dictionary")
}

func (d *nullDictionary) insertByteArray(value []byte) int32 {
	panic("cannot insert byte array value into null dictionary")
}
