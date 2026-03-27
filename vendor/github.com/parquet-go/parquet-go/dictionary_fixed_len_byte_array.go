package parquet

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type fixedLenByteArrayDictionary struct {
	fixedLenByteArrayPage
	hashmap map[string]int32
}

func newFixedLenByteArrayDictionary(typ Type, columnIndex int16, numValues int32, values encoding.Values) *fixedLenByteArrayDictionary {
	data, size := values.FixedLenByteArray()
	return &fixedLenByteArrayDictionary{
		fixedLenByteArrayPage: fixedLenByteArrayPage{
			typ:         typ,
			size:        size,
			data:        memory.SliceBufferFrom(data[:int(numValues)*size]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *fixedLenByteArrayDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *fixedLenByteArrayDictionary) Len() int { return d.data.Len() / d.size }

func (d *fixedLenByteArrayDictionary) Size() int64 { return int64(d.data.Len()) }

func (d *fixedLenByteArrayDictionary) Index(i int32) Value {
	return d.makeValueBytes(d.index(i))
}

func (d *fixedLenByteArrayDictionary) index(i int32) []byte {
	data := d.data.Slice()
	j := (int(i) + 0) * d.size
	k := (int(i) + 1) * d.size
	return data[j:k:k]
}

func (d *fixedLenByteArrayDictionary) Insert(indexes []int32, values []Value) {
	d.insertValues(indexes, len(values), func(i int) *byte {
		return values[i].ptr
	})
}

func (d *fixedLenByteArrayDictionary) insert(indexes []int32, rows sparse.Array) {
	d.insertValues(indexes, rows.Len(), func(i int) *byte {
		return (*byte)(rows.Index(i))
	})
}

func (d *fixedLenByteArrayDictionary) insertValues(indexes []int32, count int, valueAt func(int) *byte) {
	_ = indexes[:count]

	if d.hashmap == nil {
		data := d.data.Slice()
		d.hashmap = make(map[string]int32, d.data.Cap()/d.size)
		for i, j := 0, int32(0); i < len(data); i += d.size {
			d.hashmap[string(data[i:i+d.size])] = j
			j++
		}
	}

	for i := range count {
		value := unsafe.Slice(valueAt(i), d.size)

		index, exists := d.hashmap[string(value)]
		if !exists {
			index = int32(d.Len())
			start := d.data.Len()
			d.data.Append(value...)
			data := d.data.Slice()
			d.hashmap[string(data[start:])] = index
		}

		indexes[i] = index
	}
}

func (d *fixedLenByteArrayDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValueString("")
	memsetValues(values, model)
	d.lookupString(indexes, makeArrayValue(values, offsetOfPtr))
}

func (d *fixedLenByteArrayDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		base := d.index(indexes[0])
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

func (d *fixedLenByteArrayDictionary) Reset() {
	d.data.Resize(0)
	d.hashmap = nil
}

func (d *fixedLenByteArrayDictionary) Page() Page {
	return &d.fixedLenByteArrayPage
}

func (d *fixedLenByteArrayDictionary) insertBoolean(value bool) int32 {
	buf := make([]byte, d.size)
	if value {
		buf[d.size-1] = 1
	}
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertInt32(value int32) int32 {
	if d.size < 4 {
		panic(fmt.Sprintf("cannot write 4-byte int32 to fixed length byte array of size %d", d.size))
	}
	buf := make([]byte, d.size)
	binary.BigEndian.PutUint32(buf[d.size-4:], uint32(value))
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertInt64(value int64) int32 {
	if d.size < 8 {
		panic(fmt.Sprintf("cannot write 8-byte int64 to fixed length byte array of size %d", d.size))
	}
	buf := make([]byte, d.size)
	binary.BigEndian.PutUint64(buf[d.size-8:], uint64(value))
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertInt96(value deprecated.Int96) int32 {
	if d.size < 12 {
		panic(fmt.Sprintf("cannot write 12-byte int96 to fixed length byte array of size %d", d.size))
	}
	buf := make([]byte, d.size)
	binary.BigEndian.PutUint32(buf[d.size-12:d.size-8], value[2])
	binary.BigEndian.PutUint32(buf[d.size-8:d.size-4], value[1])
	binary.BigEndian.PutUint32(buf[d.size-4:], value[0])
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertFloat(value float32) int32 {
	if d.size < 4 {
		panic(fmt.Sprintf("cannot write 4-byte float to fixed length byte array of size %d", d.size))
	}
	buf := make([]byte, d.size)
	binary.BigEndian.PutUint32(buf[d.size-4:], math.Float32bits(value))
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertDouble(value float64) int32 {
	if d.size < 8 {
		panic(fmt.Sprintf("cannot write 8-byte double to fixed length byte array of size %d", d.size))
	}
	buf := make([]byte, d.size)
	binary.BigEndian.PutUint64(buf[d.size-8:], math.Float64bits(value))
	return d.insertByteArray(buf)
}

func (d *fixedLenByteArrayDictionary) insertByteArray(value []byte) int32 {
	if len(value) != d.size {
		panic(fmt.Sprintf("byte array length %d does not match fixed length %d", len(value), d.size))
	}
	indexes := [1]int32{0}
	d.insertValues(indexes[:], 1, func(i int) *byte { return &value[0] })
	return indexes[0]
}
