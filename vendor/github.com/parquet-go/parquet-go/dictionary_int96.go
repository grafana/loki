package parquet

import (
	"math/big"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/sparse"
)

type int96Dictionary struct {
	int96Page
	hashmap map[deprecated.Int96]int32
}

func newInt96Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *int96Dictionary {
	return &int96Dictionary{
		int96Page: int96Page{
			typ:         typ,
			values:      data.Int96()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *int96Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *int96Dictionary) Len() int { return len(d.values) }

func (d *int96Dictionary) Size() int64 { return int64(len(d.values) * 12) }

func (d *int96Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *int96Dictionary) index(i int32) deprecated.Int96 { return d.values[i] }

func (d *int96Dictionary) Insert(indexes []int32, values []Value) {
	d.insertValues(indexes, len(values), func(i int) deprecated.Int96 {
		return values[i].Int96()
	})
}

func (d *int96Dictionary) insert(indexes []int32, rows sparse.Array) {
	d.insertValues(indexes, rows.Len(), func(i int) deprecated.Int96 {
		return *(*deprecated.Int96)(rows.Index(i))
	})
}

func (d *int96Dictionary) insertValues(indexes []int32, count int, valueAt func(int) deprecated.Int96) {
	_ = indexes[:count]

	if d.hashmap == nil {
		d.hashmap = make(map[deprecated.Int96]int32, len(d.values))
		for i, v := range d.values {
			d.hashmap[v] = int32(i)
		}
	}

	for i := range count {
		value := valueAt(i)

		index, exists := d.hashmap[value]
		if !exists {
			index = int32(len(d.values))
			d.values = append(d.values, value)
			d.hashmap[value] = index
		}

		indexes[i] = index
	}
}

func (d *int96Dictionary) Lookup(indexes []int32, values []Value) {
	for i, j := range indexes {
		values[i] = d.Index(j)
	}
}

func (d *int96Dictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		minValue := d.index(indexes[0])
		maxValue := minValue

		for _, i := range indexes[1:] {
			value := d.index(i)
			switch {
			case value.Less(minValue):
				minValue = value
			case maxValue.Less(value):
				maxValue = value
			}
		}

		min = d.makeValue(minValue)
		max = d.makeValue(maxValue)
	}
	return min, max
}

func (d *int96Dictionary) Reset() {
	d.values = d.values[:0]
	d.hashmap = nil
}

func (d *int96Dictionary) Page() Page {
	return &d.int96Page
}

func (d *int96Dictionary) insertBoolean(value bool) int32 {
	if value {
		return d.insertInt96(deprecated.Int96{1, 0, 0})
	}
	return d.insertInt96(deprecated.Int96{0, 0, 0})
}

func (d *int96Dictionary) insertInt32(value int32) int32 {
	return d.insertInt96(deprecated.Int96{uint32(value), 0, 0})
}

func (d *int96Dictionary) insertInt64(value int64) int32 {
	return d.insertInt96(deprecated.Int64ToInt96(value))
}

func (d *int96Dictionary) insertInt96(value deprecated.Int96) int32 {
	indexes := [1]int32{0}
	d.insertValues(indexes[:], 1, func(i int) deprecated.Int96 { return value })
	return indexes[0]
}

func (d *int96Dictionary) insertFloat(value float32) int32 {
	return d.insertInt96(deprecated.Int64ToInt96(int64(value)))
}

func (d *int96Dictionary) insertDouble(value float64) int32 {
	return d.insertInt96(deprecated.Int64ToInt96(int64(value)))
}

func (d *int96Dictionary) insertByteArray(value []byte) int32 {
	v, ok := new(big.Int).SetString(string(value), 10)
	if !ok || v == nil {
		panic("invalid byte array for int96: cannot parse")
	}
	return d.insertInt96(deprecated.Int64ToInt96(v.Int64()))
}
