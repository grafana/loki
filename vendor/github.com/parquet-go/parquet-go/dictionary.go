package parquet

import (
	"io"
	"math/bits"
	"unsafe"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/hashprobe"
	"github.com/parquet-go/parquet-go/internal/bitpack"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

const (
	// Maximum load of probing tables. This parameter configures the balance
	// between memory density and compute time of probing operations. Valid
	// values are floating point numbers between 0 and 1.
	//
	// Smaller values result in lower collision probability when inserting
	// values in probing tables, but also increase memory utilization.
	//
	// TODO: make this configurable by the application?
	hashprobeTableMaxLoad = 0.85

	// An estimate of the CPU cache footprint used by insert operations.
	//
	// This constant is used to determine a useful chunk size depending on the
	// size of values being inserted in dictionaries. More values of small size
	// can fit in CPU caches, so the inserts can operate on larger chunks.
	insertsTargetCacheFootprint = 8192
)

// The Dictionary interface represents type-specific implementations of parquet
// dictionaries.
//
// Programs can instantiate dictionaries by call the NewDictionary method of a
// Type object.
//
// The current implementation has a limitation which prevents applications from
// providing custom versions of this interface because it contains unexported
// methods. The only way to create Dictionary values is to call the
// NewDictionary of Type instances. This limitation may be lifted in future
// releases.
type Dictionary interface {
	// Returns the type that the dictionary was created from.
	Type() Type

	// Returns the number of value indexed in the dictionary.
	Len() int

	// Returns the dictionary value at the given index.
	Index(index int32) Value

	// Inserts values from the second slice to the dictionary and writes the
	// indexes at which each value was inserted to the first slice.
	//
	// The method panics if the length of the indexes slice is smaller than the
	// length of the values slice.
	Insert(indexes []int32, values []Value)

	// Given an array of dictionary indexes, lookup the values into the array
	// of values passed as second argument.
	//
	// The method panics if len(indexes) > len(values), or one of the indexes
	// is negative or greater than the highest index in the dictionary.
	Lookup(indexes []int32, values []Value)

	// Returns the min and max values found in the given indexes.
	Bounds(indexes []int32) (min, max Value)

	// Resets the dictionary to its initial state, removing all values.
	Reset()

	// Returns a Page representing the content of the dictionary.
	//
	// The returned page shares the underlying memory of the buffer, it remains
	// valid to use until the dictionary's Reset method is called.
	Page() Page

	// See ColumnBuffer.writeValues for details on the use of unexported methods
	// on interfaces.
	insert(indexes []int32, rows sparse.Array)
	//lookup(indexes []int32, rows sparse.Array)
}

func checkLookupIndexBounds(indexes []int32, rows sparse.Array) {
	if rows.Len() < len(indexes) {
		panic("dictionary lookup with more indexes than values")
	}
}

// The boolean dictionary always contains two values for true and false.
type booleanDictionary struct {
	booleanPage
	// There are only two possible values for booleans, false and true.
	// Rather than using a Go map, we track the indexes of each values
	// in an array of two 32 bits integers. When inserting values in the
	// dictionary, we ensure that an index exist for each boolean value,
	// then use the value 0 or 1 (false or true) to perform a lookup in
	// the dictionary's map.
	table [2]int32
}

func newBooleanDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *booleanDictionary {
	indexOfFalse, indexOfTrue, values := int32(-1), int32(-1), data.Boolean()

	for i := int32(0); i < numValues && indexOfFalse < 0 && indexOfTrue < 0; i += 8 {
		v := values[i]
		if v != 0x00 {
			indexOfTrue = i + int32(bits.TrailingZeros8(v))
		}
		if v != 0xFF {
			indexOfFalse = i + int32(bits.TrailingZeros8(^v))
		}
	}

	return &booleanDictionary{
		booleanPage: booleanPage{
			typ:         typ,
			bits:        values[:bitpack.ByteCount(uint(numValues))],
			numValues:   numValues,
			columnIndex: ^columnIndex,
		},
		table: [2]int32{
			0: indexOfFalse,
			1: indexOfTrue,
		},
	}
}

func (d *booleanDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *booleanDictionary) Len() int { return int(d.numValues) }

func (d *booleanDictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *booleanDictionary) index(i int32) bool { return d.valueAt(int(i)) }

func (d *booleanDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfBool))
}

func (d *booleanDictionary) insert(indexes []int32, rows sparse.Array) {
	_ = indexes[:rows.Len()]

	if d.table[0] < 0 {
		d.table[0] = d.numValues
		d.numValues++
		d.bits = plain.AppendBoolean(d.bits, int(d.table[0]), false)
	}

	if d.table[1] < 0 {
		d.table[1] = d.numValues
		d.numValues++
		d.bits = plain.AppendBoolean(d.bits, int(d.table[1]), true)
	}

	values := rows.Uint8Array()
	dict := d.table

	for i := 0; i < rows.Len(); i++ {
		v := values.Index(i) & 1
		indexes[i] = dict[v]
	}
}

func (d *booleanDictionary) Lookup(indexes []int32, values []Value) {
	model := d.makeValue(false)
	memsetValues(values, model)
	d.lookup(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *booleanDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*bool)(rows.Index(i)) = d.index(j)
	}
}

func (d *booleanDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) > 0 {
		hasFalse, hasTrue := false, false

		for _, i := range indexes {
			v := d.index(i)
			if v {
				hasTrue = true
			} else {
				hasFalse = true
			}
			if hasTrue && hasFalse {
				break
			}
		}

		min = d.makeValue(!hasFalse)
		max = d.makeValue(hasTrue)
	}
	return min, max
}

func (d *booleanDictionary) Reset() {
	d.bits = d.bits[:0]
	d.offset = 0
	d.numValues = 0
	d.table = [2]int32{-1, -1}
}

func (d *booleanDictionary) Page() Page {
	return &d.booleanPage
}

type int32Dictionary struct {
	int32Page
	table *hashprobe.Int32Table
}

func newInt32Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *int32Dictionary {
	return &int32Dictionary{
		int32Page: int32Page{
			typ:         typ,
			values:      data.Int32()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *int32Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *int32Dictionary) Len() int { return len(d.values) }

func (d *int32Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *int32Dictionary) index(i int32) int32 { return d.values[i] }

func (d *int32Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *int32Dictionary) init(indexes []int32) {
	d.table = hashprobe.NewInt32Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
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
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
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
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *int32Dictionary) Page() Page {
	return &d.int32Page
}

type int64Dictionary struct {
	int64Page
	table *hashprobe.Int64Table
}

func newInt64Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *int64Dictionary {
	return &int64Dictionary{
		int64Page: int64Page{
			typ:         typ,
			values:      data.Int64()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *int64Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *int64Dictionary) Len() int { return len(d.values) }

func (d *int64Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *int64Dictionary) index(i int32) int64 { return d.values[i] }

func (d *int64Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *int64Dictionary) init(indexes []int32) {
	d.table = hashprobe.NewInt64Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
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
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
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
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *int64Dictionary) Page() Page {
	return &d.int64Page
}

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

	for i := 0; i < count; i++ {
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

type doubleDictionary struct {
	doublePage
	table *hashprobe.Float64Table
}

func newDoubleDictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *doubleDictionary {
	return &doubleDictionary{
		doublePage: doublePage{
			typ:         typ,
			values:      data.Double()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *doubleDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *doubleDictionary) Len() int { return len(d.values) }

func (d *doubleDictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *doubleDictionary) index(i int32) float64 { return d.values[i] }

func (d *doubleDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *doubleDictionary) init(indexes []int32) {
	d.table = hashprobe.NewFloat64Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
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
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
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
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *doubleDictionary) Page() Page {
	return &d.doublePage
}

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
			values:      values,
			offsets:     offsets,
			columnIndex: ^columnIndex,
		},
	}
}

func (d *byteArrayDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *byteArrayDictionary) Len() int { return d.len() }

func (d *byteArrayDictionary) Index(i int32) Value { return d.makeValueBytes(d.index(int(i))) }

func (d *byteArrayDictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfPtr))
}

func (d *byteArrayDictionary) init() {
	numValues := d.len()
	d.table = make(map[string]int32, numValues)

	for i := 0; i < numValues; i++ {
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
			d.values = append(d.values, value...)
			d.offsets = append(d.offsets, uint32(len(d.values)))
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
			d.lookupString(indexes[i:j:j], makeArrayString(values[:n:n]))

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
	d.offsets = d.offsets[:1]
	d.values = d.values[:0]
	for k := range d.table {
		delete(d.table, k)
	}
	d.alloc.reset()
}

func (d *byteArrayDictionary) Page() Page {
	return &d.byteArrayPage
}

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
			data:        data,
			columnIndex: ^columnIndex,
		},
	}
}

func (d *fixedLenByteArrayDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *fixedLenByteArrayDictionary) Len() int { return len(d.data) / d.size }

func (d *fixedLenByteArrayDictionary) Index(i int32) Value {
	return d.makeValueBytes(d.index(i))
}

func (d *fixedLenByteArrayDictionary) index(i int32) []byte {
	j := (int(i) + 0) * d.size
	k := (int(i) + 1) * d.size
	return d.data[j:k:k]
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
		d.hashmap = make(map[string]int32, cap(d.data)/d.size)
		for i, j := 0, int32(0); i < len(d.data); i += d.size {
			d.hashmap[string(d.data[i:i+d.size])] = j
			j++
		}
	}

	for i := 0; i < count; i++ {
		value := unsafe.Slice(valueAt(i), d.size)

		index, exists := d.hashmap[string(value)]
		if !exists {
			index = int32(d.Len())
			start := len(d.data)
			d.data = append(d.data, value...)
			d.hashmap[string(d.data[start:])] = index
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
			d.lookupString(indexes[i:j:j], makeArrayString(values[:n:n]))

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
	d.data = d.data[:0]
	d.hashmap = nil
}

func (d *fixedLenByteArrayDictionary) Page() Page {
	return &d.fixedLenByteArrayPage
}

type uint32Dictionary struct {
	uint32Page
	table *hashprobe.Uint32Table
}

func newUint32Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *uint32Dictionary {
	return &uint32Dictionary{
		uint32Page: uint32Page{
			typ:         typ,
			values:      data.Uint32()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *uint32Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *uint32Dictionary) Len() int { return len(d.values) }

func (d *uint32Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *uint32Dictionary) index(i int32) uint32 { return d.values[i] }

func (d *uint32Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU32))
}

func (d *uint32Dictionary) init(indexes []int32) {
	d.table = hashprobe.NewUint32Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
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
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
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
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *uint32Dictionary) Page() Page {
	return &d.uint32Page
}

type uint64Dictionary struct {
	uint64Page
	table *hashprobe.Uint64Table
}

func newUint64Dictionary(typ Type, columnIndex int16, numValues int32, data encoding.Values) *uint64Dictionary {
	return &uint64Dictionary{
		uint64Page: uint64Page{
			typ:         typ,
			values:      data.Uint64()[:numValues],
			columnIndex: ^columnIndex,
		},
	}
}

func (d *uint64Dictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *uint64Dictionary) Len() int { return len(d.values) }

func (d *uint64Dictionary) Index(i int32) Value { return d.makeValue(d.index(i)) }

func (d *uint64Dictionary) index(i int32) uint64 { return d.values[i] }

func (d *uint64Dictionary) Insert(indexes []int32, values []Value) {
	d.insert(indexes, makeArrayValue(values, offsetOfU64))
}

func (d *uint64Dictionary) init(indexes []int32) {
	d.table = hashprobe.NewUint64Table(len(d.values), hashprobeTableMaxLoad)

	n := min(len(d.values), len(indexes))

	for i := 0; i < len(d.values); i += n {
		j := min(i+n, len(d.values))
		d.table.Probe(d.values[i:j:j], indexes[:n:n])
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
				if index == int32(len(d.values)) {
					d.values = append(d.values, values.Index(i+k))
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
	d.values = d.values[:0]
	if d.table != nil {
		d.table.Reset()
	}
}

func (d *uint64Dictionary) Page() Page {
	return &d.uint64Page
}

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

// indexedType is a wrapper around a Type value which overrides object
// constructors to use indexed versions referencing values in the dictionary
// instead of storing plain values.
type indexedType struct {
	Type
	dict Dictionary
}

func newIndexedType(typ Type, dict Dictionary) *indexedType {
	return &indexedType{Type: typ, dict: dict}
}

func (t *indexedType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newIndexedColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t *indexedType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newIndexedPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

// indexedPage is an implementation of the Page interface which stores
// indexes instead of plain value. The indexes reference the values in a
// dictionary that the page was created for.
type indexedPage struct {
	typ         *indexedType
	values      []int32
	columnIndex int16
}

func newIndexedPage(typ *indexedType, columnIndex int16, numValues int32, data encoding.Values) *indexedPage {
	// RLE encoded values that contain dictionary indexes in data pages are
	// sometimes truncated when they contain only zeros. We account for this
	// special case here and extend the values buffer if it is shorter than
	// needed to hold `numValues`.
	size := int(numValues)
	values := data.Int32()

	if len(values) < size {
		if cap(values) < size {
			tmp := make([]int32, size)
			copy(tmp, values)
			values = tmp
		} else {
			clear := values[len(values):size]
			for i := range clear {
				clear[i] = 0
			}
		}
	}

	return &indexedPage{
		typ:         typ,
		values:      values[:size],
		columnIndex: ^columnIndex,
	}
}

func (page *indexedPage) Type() Type { return indexedPageType{page.typ} }

func (page *indexedPage) Column() int { return int(^page.columnIndex) }

func (page *indexedPage) Dictionary() Dictionary { return page.typ.dict }

func (page *indexedPage) NumRows() int64 { return int64(len(page.values)) }

func (page *indexedPage) NumValues() int64 { return int64(len(page.values)) }

func (page *indexedPage) NumNulls() int64 { return 0 }

func (page *indexedPage) Size() int64 { return 4 * int64(len(page.values)) }

func (page *indexedPage) RepetitionLevels() []byte { return nil }

func (page *indexedPage) DefinitionLevels() []byte { return nil }

func (page *indexedPage) Data() encoding.Values { return encoding.Int32Values(page.values) }

func (page *indexedPage) Values() ValueReader { return &indexedPageValues{page: page} }

func (page *indexedPage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		min, max = page.typ.dict.Bounds(page.values)
		min.columnIndex = page.columnIndex
		max.columnIndex = page.columnIndex
	}
	return min, max, ok
}

func (page *indexedPage) Slice(i, j int64) Page {
	return &indexedPage{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

// indexedPageType is an adapter for the indexedType returned when accessing
// the type of an indexedPage value. It overrides the Encode/Decode methods to
// account for the fact that an indexed page is holding indexes of values into
// its dictionary instead of plain values.
type indexedPageType struct{ *indexedType }

func (t indexedPageType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.Int32ValuesFromBytes(values)
}

func (t indexedPageType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeInt32(dst, src, enc)
}

func (t indexedPageType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeInt32(dst, src, enc)
}

func (t indexedPageType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return Int32Type.EstimateDecodeSize(numValues, src, enc)
}

type indexedPageValues struct {
	page   *indexedPage
	offset int
}

func (r *indexedPageValues) ReadValues(values []Value) (n int, err error) {
	if n = len(r.page.values) - r.offset; n == 0 {
		return 0, io.EOF
	}
	if n > len(values) {
		n = len(values)
	}
	r.page.typ.dict.Lookup(r.page.values[r.offset:r.offset+n], values[:n])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

// indexedColumnBuffer is an implementation of the ColumnBuffer interface which
// builds a page of indexes into a parent dictionary when values are written.
type indexedColumnBuffer struct{ indexedPage }

func newIndexedColumnBuffer(typ *indexedType, columnIndex int16, numValues int32) *indexedColumnBuffer {
	return &indexedColumnBuffer{
		indexedPage: indexedPage{
			typ:         typ,
			values:      make([]int32, 0, numValues),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *indexedColumnBuffer) Clone() ColumnBuffer {
	return &indexedColumnBuffer{
		indexedPage: indexedPage{
			typ:         col.typ,
			values:      append([]int32{}, col.values...),
			columnIndex: col.columnIndex,
		},
	}
}

func (col *indexedColumnBuffer) Type() Type { return col.typ.Type }

func (col *indexedColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return indexedColumnIndex{col}, nil
}

func (col *indexedColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return indexedOffsetIndex{col}, nil
}

func (col *indexedColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *indexedColumnBuffer) Dictionary() Dictionary { return col.typ.dict }

func (col *indexedColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *indexedColumnBuffer) Page() Page { return &col.indexedPage }

func (col *indexedColumnBuffer) Reset() { col.values = col.values[:0] }

func (col *indexedColumnBuffer) Cap() int { return cap(col.values) }

func (col *indexedColumnBuffer) Len() int { return len(col.values) }

func (col *indexedColumnBuffer) Less(i, j int) bool {
	u := col.typ.dict.Index(col.values[i])
	v := col.typ.dict.Index(col.values[j])
	return col.typ.Compare(u, v) < 0
}

func (col *indexedColumnBuffer) Swap(i, j int) {
	col.values[i], col.values[j] = col.values[j], col.values[i]
}

func (col *indexedColumnBuffer) WriteValues(values []Value) (int, error) {
	i := len(col.values)
	j := len(col.values) + len(values)

	if j <= cap(col.values) {
		col.values = col.values[:j]
	} else {
		tmp := make([]int32, j, 2*j)
		copy(tmp, col.values)
		col.values = tmp
	}

	col.typ.dict.Insert(col.values[i:], values)
	return len(values), nil
}

func (col *indexedColumnBuffer) writeValues(rows sparse.Array, _ columnLevels) {
	i := len(col.values)
	j := len(col.values) + rows.Len()

	if j <= cap(col.values) {
		col.values = col.values[:j]
	} else {
		tmp := make([]int32, j, 2*j)
		copy(tmp, col.values)
		col.values = tmp
	}

	col.typ.dict.insert(col.values[i:], rows)
}

func (col *indexedColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.values)))
	case i >= len(col.values):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.values) {
			values[n] = col.typ.dict.Index(col.values[i])
			values[n].columnIndex = col.columnIndex
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

func (col *indexedColumnBuffer) ReadRowAt(row Row, index int64) (Row, error) {
	switch {
	case index < 0:
		return row, errRowIndexOutOfBounds(index, int64(len(col.values)))
	case index >= int64(len(col.values)):
		return row, io.EOF
	default:
		v := col.typ.dict.Index(col.values[index])
		v.columnIndex = col.columnIndex
		return append(row, v), nil
	}
}

type indexedColumnIndex struct{ col *indexedColumnBuffer }

func (index indexedColumnIndex) NumPages() int       { return 1 }
func (index indexedColumnIndex) NullCount(int) int64 { return 0 }
func (index indexedColumnIndex) NullPage(int) bool   { return false }
func (index indexedColumnIndex) MinValue(int) Value {
	min, _, _ := index.col.Bounds()
	return min
}
func (index indexedColumnIndex) MaxValue(int) Value {
	_, max, _ := index.col.Bounds()
	return max
}
func (index indexedColumnIndex) IsAscending() bool {
	min, max, _ := index.col.Bounds()
	return index.col.typ.Compare(min, max) <= 0
}
func (index indexedColumnIndex) IsDescending() bool {
	min, max, _ := index.col.Bounds()
	return index.col.typ.Compare(min, max) > 0
}

type indexedOffsetIndex struct{ col *indexedColumnBuffer }

func (index indexedOffsetIndex) NumPages() int                { return 1 }
func (index indexedOffsetIndex) Offset(int) int64             { return 0 }
func (index indexedOffsetIndex) CompressedPageSize(int) int64 { return index.col.Size() }
func (index indexedOffsetIndex) FirstRowIndex(int) int64      { return 0 }
