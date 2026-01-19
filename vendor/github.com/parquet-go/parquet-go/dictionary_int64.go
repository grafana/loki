package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type int64Dictionary struct {
	int64Page
	table *hashprobe.Int64Table
}

func newInt64Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *int64Dictionary {
	return &int64Dictionary{
		int64Page: int64Page{
			typ:         typ,
			values:      memory.SliceBufferFrom(data.Int64()[:numValues]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *int64Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *int64Dictionary) Len() int { return d.values.Len() }

func (d *int64Dictionary) Size() int64 { return int64(d.values.Len() * 8) }

func (d *int64Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *int64Dictionary) index(i int32) int64 { return d.values.Slice()[i] }

func (d *int64Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *int64Dictionary) init(indexes []int32) {
	values := d.values.Slice()
	d.table = hashprobe.NewInt64Table(len(values), hashprobeTableMaxLoad)

	n := min(len(values), len(indexes))

	for i := 0; i < len(values); i += n {
		j := min(i+n, len(values))
		d.table.Probe(values[i:j:j], indexes[:n:n])
	}
}

func (d *int64Dictionary) insert(indexes []int32, rows sparse.Array) {
	const chunkSize = insertsTargetCacheFootprint / 8

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Int64Array()

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

func (d *int64Dictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *int64Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *int64Dictionary) Reset() {
	d.values.Reset()
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *int64Dictionary) Page() Page {
	return &d.int64Page
}

func (d *int64Dictionary) insertBoolean(value bool) int32 {
	panic("cannot insert boolean value into int64 dictionary")
}

func (d *int64Dictionary) insertInt32(value int32) int32 {
	return d.insertInt64(int64(value))
}

func (d *int64Dictionary) insertInt64(value int64) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}

func (d *int64Dictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertInt64(value.Int64())
}

func (d *int64Dictionary) insertFloat(value float32) int32 {
	return d.insertInt64(int64(value))
}

func (d *int64Dictionary) insertDouble(value float64) int32 {
	return d.insertInt64(int64(value))
}

func (d *int64Dictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		panic(err)
	}
	return d.insertInt64(v)
}
