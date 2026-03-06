package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type doubleDictionary struct {
	doublePage
	table *hashprobe.Float64Table
}

func newDoubleDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *doubleDictionary {
	return &doubleDictionary{
		doublePage: doublePage{
			typ:         typ,
			values:      memory.SliceBufferFrom(data.Double()[:numValues]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *doubleDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *doubleDictionary) Len() int { return d.values.Len() }

func (d *doubleDictionary) Size() int64 { return int64(d.values.Len() * 8) }

func (d *doubleDictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *doubleDictionary) index(i int32) float64 { return d.values.Slice()[i] }

func (d *doubleDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *doubleDictionary) init(indexes []int32) {
	values := d.values.Slice()
	d.table = hashprobe.NewFloat64Table(len(values), hashprobeTableMaxLoad)

	n := min(len(values), len(indexes))

	for i := 0; i < len(values); i += n {
		j := min(i+n, len(values))
		d.table.Probe(values[i:j:j], indexes[:n:n])
	}
}

func (d *doubleDictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 8

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Float64Array()

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

func (d *doubleDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *doubleDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *doubleDictionary) Reset() {
	d.values.Reset()
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *doubleDictionary) Page() Page {
	return &d.doublePage
}

func (d *doubleDictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertDouble(1)
	}
	return d.insertDouble(0)
}

func (d *doubleDictionary) insertInt32(value int32) int32 {
	return d.insertDouble(float64(value))
}

func (d *doubleDictionary) insertInt64(value int64) int32 {
	return d.insertDouble(float64(value))
}

func (d *doubleDictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertDouble(float64(value.Int64()))
}

func (d *doubleDictionary) insertFloat(value float32) int32 {
	return d.insertDouble(float64(value))
}

func (d *doubleDictionary) insertDouble(value float64) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}

func (d *doubleDictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseUint(string(value), 10, 32)
	if err != nil {
		panic(err)
	}
	return d.insertDouble(float64(v))
}
