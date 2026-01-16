package parquet

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/sparse"
)

type be128Dictionary struct {
	be128Page
	table *hashprobe.Uint128Table
}

func newBE128Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *be128Dictionary {
	return &be128Dictionary{
		be128Page: be128Page{
			typ:         typ,
			values:      data.Uint128()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *be128Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *be128Dictionary) Len() int { return len(d.values) }

func (d *be128Dictionary) Size() int64 { return int64(len(d.values) * 16) }

func (d *be128Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *be128Dictionary) index(i int32) *[16]byte { return &d.values[i] }

func (d *be128Dictionary) Insert(indexes []int32, values []Value) {
	_ = indexes[:len(values)]

	for _, v := range values {
		if v.kind != ^int8(FixedLenByteArray) {
			panic("values inserted in BE128 dictionary must be of type BYTE_ARRAY")
		}
		if v.u64 != 16 {
			panic("values inserted in BE128 dictionary must be of length 16")
		}
	}

	if d.table == nil {
		d.init(indexes)
	}

	const chunkSize = insertsTargetCacheFootprint / 16
	var buffer [chunkSize][16]byte

	for i := 0; i < len(values); i += chunkSize {
		j := min(chunkSize+i, len(values))
		n := min(chunkSize, len(values)-i)

		probe := buffer[:n:n]
		writePointersBE128(probe, makeArrayValue(values[i:j], unsafe.Offsetof(values[i].ptr)))

		if d.table.Probe(probe, indexes[i:j:j]) > 0 {
			for k, v := range probe {
				if indexes[i+k] == int32(len(d.values)) {
					d.values = append(d.values, v)
				}
			}
		}
	}
}

func (d *be128Dictionary) init(indexes []int32) {
	d.table = hashprobe.NewUint128Table(len(d.values), 0.75)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
	}
}

func (d *be128Dictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 16

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Uint128Array()

	for i := 0; i < values.Len(); i += chunkSize {
		j := min(i+chunkSize, values.Len())

		if d.table.ProbeArray(values.Slice(i, j), indexes[i:j:j]) > 0 {
			for k, index := range indexes[i:j] {
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
				}
			}
		}
	}
}

func (d *be128Dictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValueString("")
	memsetValues(values, model)
	d.lookupString(indexes, makeArrayValue(values, offsetOfPtr))
}

func (d *be128Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *be128Dictionary) Reset() {
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *be128Dictionary) Page() Page {
	return &d.be128Page
}

func (d *be128Dictionary) insertBoolean(value bool) int32 {
	var buf [16]byte
	if value {
		buf[15] = 1
	}
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertInt32(value int32) int32 {
	var buf [16]byte
	binary.BigEndian.PutUint32(buf[12:16], uint32(value))
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertInt64(value int64) int32 {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[8:16], uint64(value))
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertInt96(value deprecated.Int96) int32 {
	var buf [16]byte
	binary.BigEndian.PutUint32(buf[4:8], value[2])
	binary.BigEndian.PutUint32(buf[8:12], value[1])
	binary.BigEndian.PutUint32(buf[12:16], value[0])
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertFloat(value float32) int32 {
	var buf [16]byte
	binary.BigEndian.PutUint32(buf[12:16], math.Float32bits(value))
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertDouble(value float64) int32 {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[8:16], math.Float64bits(value))
	return d.insertByteArray(buf[:])
}

func (d *be128Dictionary) insertByteArray(value []byte) int32 {
	if len(value) != 16 {
		panic(fmt.Sprintf("byte array length %d does not match required length 16 for be128", len(value)))
	}

	b := ([16]byte)(value)
	i := [1]int32{}
	d.insert(i[:], makeArrayFromPointer(&b))
	return i[0]
}
