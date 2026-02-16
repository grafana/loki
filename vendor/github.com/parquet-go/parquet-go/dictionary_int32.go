package parquet

import (
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type int32Dictionary struct {
	int32Page
	table *hashprobe.Int32Table
}

func newInt32Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *int32Dictionary {
	return &int32Dictionary{
		int32Page: int32Page{
			typ:         typ,
			values:      memory.SliceBufferFrom(data.Int32()[:numValues]),
			columnIndex: ^columnIndex,
		},
	}
}

func (d *int32Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *int32Dictionary) Len() int { return d.values.Len() }

func (d *int32Dictionary) Size() int64 { return int64(d.values.Len() * 4) }

func (d *int32Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *int32Dictionary) index(i int32) int32 { return d.values.Slice()[i] }

func (d *int32Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *int32Dictionary) init(indexes []int32) {
	values := d.values.Slice()
	d.table = hashprobe.NewInt32Table(len(values), hashprobeTableMaxLoad)

	n := min(len(values), len(indexes))

	for i := 0; i < len(values); i += n {
		j := min(i+n, len(values))
		d.table.Probe(values[i:j:j], indexes[:n:n])
	}
}

func (d *int32Dictionary) insert(indexes []int32, rows sparse.Array) {
	// Iterating over the input in chunks helps keep relevant data in CPU
	// caches when a large number of values are inserted into the dictionary with
	// a single method call.
	//
	// Without this chunking, memory areas from the head of the indexes and
	// values arrays end up being evicted from CPU caches as the probing
	// operation iterates through the array. The subsequent scan of the indexes
	// required to determine which values must be inserted into the page then
	// stalls on retrieving data from main memory.
	//
	// We measured as much as ~37% drop in throughput when disabling the
	// chunking, and did not observe any penalties from having it on smaller
	// inserts.
	const chunkSize = insertsTargetCacheFootprint / 4

	if d.table == nil {
		d.init(indexes)
	}

	values := rows.Int32Array()

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

func (d *int32Dictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(0)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *int32Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue, maxValue := d.bounds(indexes)
		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *int32Dictionary) Reset() {
	d.values.Reset()
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *int32Dictionary) Page() Page {
	return &d.int32Page
}

func (d *int32Dictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertInt32(1)
	}
	return d.insertInt32(0)
}

func (d *int32Dictionary) insertInt32(value int32) int32 {
	var indexes [1]int32
	d.insert(indexes[:], makeArrayFromPointer(&value))
	return indexes[0]
}

func (d *int32Dictionary) insertInt64(value int64) int32 {
	return d.insertInt32(int32(value))
}

func (d *int32Dictionary) insertInt96(value deprecated.Int96) int32 {
	return d.insertInt32(int32(value[0]))
}

func (d *int32Dictionary) insertFloat(value float32) int32 {
	return d.insertInt32(int32(value))
}

func (d *int32Dictionary) insertDouble(value float64) int32 {
	return d.insertInt32(int32(value))
}

func (d *int32Dictionary) insertByteArray(value []byte) int32 {
	v, err := strconv.ParseInt(string(value), 10, 32)
	if err != nil {
		panic(err)
	}
	return d.insertInt32(int32(v))
}
