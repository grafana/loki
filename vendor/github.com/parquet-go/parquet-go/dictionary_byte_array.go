package parquet

import (
	"strconv"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type byteArrayDictionary struct {
	byteArrayPage
	table map[string]int32
	alloc allocator
}

func newByteArrayDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *byteArrayDictionary {
	values, offsets := data.ByteArray()
	// The first offset must always be zero, and the last offset is the length
	// of the values in bytes.
	//
	// As an optimization we make the assumption that the backing array of the
	// offsets slice belongs to the dictionary.
	switch {
	case cap(offsets) == 0:
		offsets = make([]uint32, 1, 8)
	case len(offsets) == 0:
		offsets = append(offsets[:0], 0)
	}
	return &byteArrayDictionary{
		byteArrayPage: byteArrayPage{
			typ:         typ,
			values:      memory.SliceBufferFrom(values),
			offsets:     memory.SliceBufferFrom(offsets),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *byteArrayDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *byteArrayDictionary) Len() int { return d.len() }

func (d *byteArrayDictionary) Size() int64 { return int64(d.values.Len()) }

func (d *byteArrayDictionary) Index(i int32) Value { return d.makeValueBytes(d.index(int(i))) }

func (d *byteArrayDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfPtr))
}

func (d *byteArrayDictionary) init() {
	numValues := d.len()
	d.table = make(map[string]int32, numValues)

	for i := range numValues {
		d.table[string(d.index(i))] = int32(len(d.table))
	}
}

func (d *byteArrayDictionary) insert(indexes []int32, rows sparse.Array) {
	if d.table == nil {
		d.init()
	}

	values := rows.StringArray()

	for i := range indexes {
		value := values.Index(i)

		index, exists := d.table[value]
		if !exists {
			value = d.alloc.copyString(value)
			index = int32(len(d.table))
			d.table[value] = index
			d.values.Append([]byte(value)...)
			d.offsets.AppendValue(uint32(d.values.Len()))
		}

		indexes[i] = index
	}
}

func (d *byteArrayDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValueString("")
	memsetValues(values, model)
	d.lookupString(indexes, makeArrayValue(values, offsetOfPtr))
}

func (d *byteArrayDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		base := d.index(int(indexes[0]))
		minValue := unsafecast.String(base)
		maxValue := minValue
		values := [64]string{}

		for i := 1; i < len(indexes); i += len(values) {
			n := len(indexes) - i
			if n > len(values) {
				n = len(values)
			}
			j := i + n
			d.lookupString(indexes[i:j:j], makeArrayFromSlice(values[:n:n]))

			for _, value := range values[:n:n] {
				switch {
				case value < minValue:
					minValue = value
				case value > maxValue:
					maxValue = value
				}
			}
		}

		min = d.makeValueString(minValue)
		max = d.makeValueString(maxValue)
	}
	return min, max
}

func (d *byteArrayDictionary) Reset() {
	d.offsets.Resize(1)
	d.values.Resize(0)
	for k := range d.table {
		delete(d.table, k)
	}
	d.alloc.reset()
}

func (d *byteArrayDictionary) Page() Page {
	return &d.byteArrayPage
}

func (d *byteArrayDictionary) insertBoolean(value bool) int32 {
	return d.insertByteArray(strconv.AppendBool(make([]byte, 0, 8), value))
}

func (d *byteArrayDictionary) insertInt32(value int32) int32 {
	return d.insertByteArray(strconv.AppendInt(make([]byte, 0, 16), int64(value), 10))
}

func (d *byteArrayDictionary) insertInt64(value int64) int32 {
	return d.insertByteArray(strconv.AppendInt(make([]byte, 0, 24), value, 10))
}

func (d *byteArrayDictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertByteArray([]byte(value.String()))
}

func (d *byteArrayDictionary) insertFloat(value float32) int32 {
	return d.insertByteArray(strconv.AppendFloat(make([]byte, 0, 24), float64(value), 'g', -1, 32))
}

func (d *byteArrayDictionary) insertDouble(value float64) int32 {
	return d.insertByteArray(strconv.AppendFloat(make([]byte, 0, 24), value, 'g', -1, 64))
}

func (d *byteArrayDictionary) insertByteArray(value []byte) int32 {
	s := unsafecast.String(value)
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&s))
	return indexes[0]
}
