package parquet

import (
	"io"

	"slices"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
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

	// Returns the total size in bytes of all values stored in the dictionary.
	// This is used for tracking dictionary memory usage and enforcing size limits.
	Size() int64

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

	// Parquet primitive type insert methods. Each dictionary implementation
	// supports only the Parquet types it can handle and panics for others.
	// Returns the index at which the value was inserted (or already existed).
	insertBoolean(value bool) int32
	insertInt32(value int32) int32
	insertInt64(value int64) int32
	insertInt96(value deprecated.Int96) int32
	insertFloat(value float32) int32
	insertDouble(value float64) int32
	insertByteArray(value []byte) int32

	//lookup(indexes []int32, rows sparse.Array)
}

func checkLookupIndexBounds(indexes []int32, rows sparse.Array) {
	if rows.Len() < len(indexes) {
		panic("dictionary lookup with more indexes than values")
	}
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
			values:      slices.Clone(col.values),
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

func (col *indexedColumnBuffer) writeValues(_ columnLevels, rows sparse.Array) {
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

func (col *indexedColumnBuffer) writeBoolean(_ columnLevels, value bool) {
	col.values = append(col.values, col.typ.dict.insertBoolean(value))
}

func (col *indexedColumnBuffer) writeInt32(_ columnLevels, value int32) {
	col.values = append(col.values, col.typ.dict.insertInt32(value))
}

func (col *indexedColumnBuffer) writeInt64(_ columnLevels, value int64) {
	col.values = append(col.values, col.typ.dict.insertInt64(value))
}

func (col *indexedColumnBuffer) writeInt96(_ columnLevels, value deprecated.Int96) {
	col.values = append(col.values, col.typ.dict.insertInt96(value))
}

func (col *indexedColumnBuffer) writeFloat(_ columnLevels, value float32) {
	col.values = append(col.values, col.typ.dict.insertFloat(value))
}

func (col *indexedColumnBuffer) writeDouble(_ columnLevels, value float64) {
	col.values = append(col.values, col.typ.dict.insertDouble(value))
}

func (col *indexedColumnBuffer) writeByteArray(_ columnLevels, value []byte) {
	col.values = append(col.values, col.typ.dict.insertByteArray(value))
}

func (col *indexedColumnBuffer) writeNull(_ columnLevels) {
	panic("cannot write null to indexed column")
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
