package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type uint64Dictionary struct {
	uint64Page
	table *hashprobe.Uint64Table
}

func newUint64Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *uint64Dictionary {
	return &uint64Dictionary{
		uint64Page: uint64Page{
			typ:         typ,
			values:      memory.SliceBufferFrom(data.Uint64()[:numValues]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *uint64Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *uint64Dictionary) Len() int { return d.values.Len() }

func (d *uint64Dictionary) Size() int64 { return int64(d.values.Len() * 8) }

func (d *uint64Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *uint64Dictionary) index(i int32) uint64 { return d.values.Slice()[i] }

func (d *uint64Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *uint64Dictionary) init(indexes []int32) {
	values := d.values.Slice()
	d.table = hashprobe.NewUint64Table(len(values), hashprobeTableMaxLoad)

	n := min(len(values), len(indexes))

	for i := 0; i < len(values); i += n {
		j := min(i+n, len(values))
		d.table.Probe(values[i:j:j], indexes[:n:n])
	}
}

func (d *uint64Dictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 8

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Uint64Array()

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

func (d *uint64Dictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *uint64Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *uint64Dictionary) Reset() {
	d.values.Reset()
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *uint64Dictionary) Page() Page {
	return &d.uint64Page
}

func (d *uint64Dictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertUint64(1)
	}
	return d.insertUint64(0)
}

func (d *uint64Dictionary) insertInt32(value int32) int32 {
	return d.insertUint64(uint64(value))
}

func (d *uint64Dictionary) insertInt64(value int64) int32 {
	return d.insertUint64(uint64(value))
}

func (d *uint64Dictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertUint64(uint64(value.Int64()))
}

func (d *uint64Dictionary) insertFloat(value float32) int32 {
	return d.insertUint64(uint64(value))
}

func (d *uint64Dictionary) insertDouble(value float64) int32 {
	return d.insertUint64(uint64(value))
}

func (d *uint64Dictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseUint(string(value), 10, 32)
	if err != nil {
		panic(err)
	}
	return d.insertUint64(uint64(v))
}

func (d *uint64Dictionary) insertUint64(value uint64) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}
