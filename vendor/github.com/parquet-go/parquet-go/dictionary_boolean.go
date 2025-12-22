package parquet

import (
	"math/bits"

	"github.com/parquet-go/bitpack"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

// The boolean dictionary always contains two values for true and false.
type booleanDictionary struct {
	booleanPage
	// There are only two possible values for booleans, false and true.
	// Rather than using a Go map, we track the indexes of each values
	// in an array of two 32 bits integers. When inserting values in the
	// dictionary, we ensure that an index exist for each boolean value,
	// then use the value 0 or 1 (false or true) to perform a lookup in
	// the dictionary's map.
	table [2]int32
}

func newBooleanDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *booleanDictionary {
	indexOfFalse, indexOfTrue, values := int32(-1), int32(-1), data.Boolean()

	for i := int32(0); i < numValues && indexOfFalse < 0 && indexOfTrue < 0; i += 8 {
		v := values[i]
		if v != 0x00 {
			indexOfTrue = i + int32(bits.TrailingZeros8(v))
		}
		if v != 0xFF {
			indexOfFalse = i + int32(bits.TrailingZeros8(^v))
		}
	}

	return &booleanDictionary{
		booleanPage: booleanPage{
			typ:         typ,
			bits:        memory.SliceBufferFrom(values[:bitpack.ByteCount(uint(numValues))]),
			numValues:   numValues,
			columnIndex: ^columnIndex,
		},
		table: [2]int32{
			0: indexOfFalse,
			1: indexOfTrue,
		},
	}
}

func (d *booleanDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *booleanDictionary) Len() int { return int(d.numValues) }

func (d *booleanDictionary) Size() int64 { return int64(d.bits.Len()) }

func (d *booleanDictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *booleanDictionary) index(i int32) bool { return d.valueAt(int(i)) }

func (d *booleanDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfBool))
}

func (d *booleanDictionary) insert(indexes []int32, rows sparse.Array) {
	_ = indexes[:rows.Len()]

	if d.table[0] < 0 {
		d.table[0] = d.numValues
		d.numValues++
		bits := plain.AppendBoolean(d.bits.Slice(), int(d.table[0]), false)
		d.bits = memory.SliceBufferFrom(bits)
	}

	if d.table[1] < 0 {
		d.table[1] = d.numValues
		d.numValues++
		bits := plain.AppendBoolean(d.bits.Slice(), int(d.table[1]), true)
		d.bits = memory.SliceBufferFrom(bits)
	}

	values := rows.Uint8Array()
	dict := d.table

	for i := range rows.Len() {
		v := values.Index(i) & 1
		indexes[i] = dict[v]
	}
}

func (d *booleanDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(false)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *booleanDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*bool)(rows.Index(i)) = d.index(j)
	}
}

func (d *booleanDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		hasFalse, hasTrue := false, false

		for _, i := range indexes {
			v := d.index(i)
			if v {
				hasTrue = true
			} else {
				hasFalse = true
			}
			if hasTrue && hasFalse {
				break
			}
		}

		min = d.makeValue(!hasFalse)
		max = d.makeValue(hasTrue)
	}
	return min, max
}

func (d *booleanDictionary) Reset() {
	d.bits.Reset()
	d.offset = 0
	d.numValues = 0
	d.table = [2]int32{-1, -1}
}

func (d *booleanDictionary) Page() Page {
	return &d.booleanPage
}

func (d *booleanDictionary) insertBoolean(value bool) int32 {
	// Ensure both indexes are initialized
	if d.table[0] < 0 {
		d.table[0] = d.numValues
		d.numValues++
		bits := plain.AppendBoolean(d.bits.Slice(), int(d.table[0]), false)
		d.bits = memory.SliceBufferFrom(bits)
	}
	if d.table[1] < 0 {
		d.table[1] = d.numValues
		d.numValues++
		bits := plain.AppendBoolean(d.bits.Slice(), int(d.table[1]), true)
		d.bits = memory.SliceBufferFrom(bits)
	}
	if value {
		return d.table[1]
	}
	return d.table[0]
}

func (d *booleanDictionary) insertInt32(value int32) int32 {
	return d.insertBoolean(value != 0)
}

func (d *booleanDictionary) insertInt64(value int64) int32 {
	return d.insertBoolean(value != 0)
}

func (d *booleanDictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertBoolean(!value.IsZero())
}

func (d *booleanDictionary) insertFloat(value float32) int32 {
	return d.insertBoolean(value != 0)
}

func (d *booleanDictionary) insertDouble(value float64) int32 {
	return d.insertBoolean(value != 0)
}

func (d *booleanDictionary) insertByteArray(value []byte) int32 {
	return d.insertBoolean(len(value) != 0)
}
