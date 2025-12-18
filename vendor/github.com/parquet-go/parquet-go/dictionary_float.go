package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/sparse"
)

type floatDictionary struct {
	floatPage
	table *hashprobe.Float32Table
}

func newFloatDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *floatDictionary {
	return &floatDictionary{
		floatPage: floatPage{
			typ:         typ,
			values:      data.Float()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *floatDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *floatDictionary) Len() int { return len(d.values) }

func (d *floatDictionary) Size() int64 { return int64(len(d.values) * 4) }

func (d *floatDictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *floatDictionary) index(i int32) float32 { return d.values[i] }

func (d *floatDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *floatDictionary) init(indexes []int32) {
	d.table = hashprobe.NewFloat32Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
	}
}

func (d *floatDictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 4

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Float32Array()

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

func (d *floatDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *floatDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *floatDictionary) Reset() {
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *floatDictionary) Page() Page {
	return &d.floatPage
}

func (d *floatDictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertFloat(1)
	}
	return d.insertFloat(0)
}

func (d *floatDictionary) insertInt32(value int32) int32 {
	return d.insertFloat(float32(value))
}

func (d *floatDictionary) insertInt64(value int64) int32 {
	return d.insertFloat(float32(value))
}

func (d *floatDictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertFloat(float32(value.Int64()))
}

func (d *floatDictionary) insertFloat(value float32) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}

func (d *floatDictionary) insertDouble(value float64) int32 {
	return d.insertFloat(float32(value))
}

func (d *floatDictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseFloat(string(value), 32)
	if err != nil {
		panic(err)
	}
	return d.insertFloat(float32(v))
}
