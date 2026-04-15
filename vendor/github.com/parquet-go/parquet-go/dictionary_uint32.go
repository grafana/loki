package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type uint32Dictionary struct {
	uint32Page
	table *hashprobe.Uint32Table
}

func newUint32Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *uint32Dictionary {
	return &uint32Dictionary{
		uint32Page: uint32Page{
			typ:         typ,
			values:      memory.SliceBufferFrom(data.Uint32()[:numValues]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *uint32Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *uint32Dictionary) Len() int { return d.values.Len() }

func (d *uint32Dictionary) Size() int64 { return int64(d.values.Len() * 4) }

func (d *uint32Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *uint32Dictionary) index(i int32) uint32 { return d.values.Slice()[i] }

func (d *uint32Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *uint32Dictionary) init(indexes []int32) {
	values := d.values.Slice()
	d.table = hashprobe.NewUint32Table(len(values), hashprobeTableMaxLoad)

	n := min(len(values), len(indexes))

	for i := 0; i < len(values); i += n {
		j := min(i+n, len(values))
		d.table.Probe(values[i:j:j], indexes[:n:n])
	}
}

func (d *uint32Dictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 4

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Uint32Array()

	for i := 0; i < values.Len(); i += chunkSize {
		j := min(i+chunkSize, values.Len())

		if d.table.ProbeArray(values.Slice(i, j), indexes[i:j:j]) > 0 {
			for k, index := range indexes[i:j] {
				if index == int32(d.values.Len()) {
					d.values.Append(values.Index(i + k))
				}
			}
		}
	}
}

func (d *uint32Dictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *uint32Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *uint32Dictionary) Reset() {
	d.values.Reset()
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *uint32Dictionary) Page() Page {
	return &d.uint32Page
}

func (d *uint32Dictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertUint32(1)
	}
	return d.insertUint32(0)
}

func (d *uint32Dictionary) insertInt32(value int32) int32 {
	return d.insertUint32(uint32(value))
}

func (d *uint32Dictionary) insertInt64(value int64) int32 {
	return d.insertUint32(uint32(value))
}

func (d *uint32Dictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertUint32(value[0])
}

func (d *uint32Dictionary) insertFloat(value float32) int32 {
	return d.insertUint32(uint32(value))
}

func (d *uint32Dictionary) insertDouble(value float64) int32 {
	return d.insertUint32(uint32(value))
}

func (d *uint32Dictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseUint(string(value), 10, 32)
	if err != nil {
		panic(err)
	}
	return d.insertUint32(uint32(v))
}

func (d *uint32Dictionary) insertUint32(value uint32) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}
