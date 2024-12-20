//go:build !purego

package parquet

import (
	"unsafe"

	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

//go:noescape
func dictionaryBoundsInt32(dict []int32, indexes []int32) (min, max int32, err errno)

//go:noescape
func dictionaryBoundsInt64(dict []int64, indexes []int32) (min, max int64, err errno)

//go:noescape
func dictionaryBoundsFloat32(dict []float32, indexes []int32) (min, max float32, err errno)

//go:noescape
func dictionaryBoundsFloat64(dict []float64, indexes []int32) (min, max float64, err errno)

//go:noescape
func dictionaryBoundsUint32(dict []uint32, indexes []int32) (min, max uint32, err errno)

//go:noescape
func dictionaryBoundsUint64(dict []uint64, indexes []int32) (min, max uint64, err errno)

//go:noescape
func dictionaryBoundsBE128(dict [][16]byte, indexes []int32) (min, max *[16]byte, err errno)

//go:noescape
func dictionaryLookup32(dict []uint32, indexes []int32, rows sparse.Array) errno

//go:noescape
func dictionaryLookup64(dict []uint64, indexes []int32, rows sparse.Array) errno

//go:noescape
func dictionaryLookupByteArrayString(dict []uint32, page []byte, indexes []int32, rows sparse.Array) errno

//go:noescape
func dictionaryLookupFixedLenByteArrayString(dict []byte, len int, indexes []int32, rows sparse.Array) errno

//go:noescape
func dictionaryLookupFixedLenByteArrayPointer(dict []byte, len int, indexes []int32, rows sparse.Array) errno

func (d *int32Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dict := unsafecast.Slice[uint32](d.values)
	dictionaryLookup32(dict, indexes, rows).check()
}

func (d *int64Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dict := unsafecast.Slice[uint64](d.values)
	dictionaryLookup64(dict, indexes, rows).check()
}

func (d *floatDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dict := unsafecast.Slice[uint32](d.values)
	dictionaryLookup32(dict, indexes, rows).check()
}

func (d *doubleDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dict := unsafecast.Slice[uint64](d.values)
	dictionaryLookup64(dict, indexes, rows).check()
}

func (d *byteArrayDictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	// TODO: this optimization is disabled for now because it appears to race
	// with the garbage collector and result in writing pointers to free objects
	// to the output.
	//
	// This command was used to trigger the problem:
	//
	//	GOMAXPROCS=8 go test -run TestIssue368 -count 10
	//
	// https://github.com/segmentio/parquet-go/issues/368
	//
	//dictionaryLookupByteArrayString(d.offsets, d.values, indexes, rows).check()
	for i, j := range indexes {
		*(*string)(rows.Index(i)) = unsafecast.String(d.index(int(j)))
	}
}

func (d *fixedLenByteArrayDictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	//dictionaryLookupFixedLenByteArrayString(d.data, d.size, indexes, rows).check()
	for i, j := range indexes {
		*(*string)(rows.Index(i)) = unsafecast.String(d.index(j))
	}
}

func (d *uint32Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dictionaryLookup32(d.values, indexes, rows).check()
}

func (d *uint64Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	dictionaryLookup64(d.values, indexes, rows).check()
}

func (d *be128Dictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	//dict := unsafecast.Slice[byte](d.values)
	//dictionaryLookupFixedLenByteArrayString(dict, 16, indexes, rows).check()
	s := "0123456789ABCDEF"
	for i, j := range indexes {
		*(**[16]byte)(unsafe.Pointer(&s)) = d.index(j)
		*(*string)(rows.Index(i)) = s
	}
}

func (d *be128Dictionary) lookupPointer(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	//dict := unsafecast.Slice[byte](d.values)
	//dictionaryLookupFixedLenByteArrayPointer(dict, 16, indexes, rows).check()
	for i, j := range indexes {
		*(**[16]byte)(rows.Index(i)) = d.index(j)
	}
}

func (d *int32Dictionary) bounds(indexes []int32) (min, max int32) {
	min, max, err := dictionaryBoundsInt32(d.values, indexes)
	err.check()
	return min, max
}

func (d *int64Dictionary) bounds(indexes []int32) (min, max int64) {
	min, max, err := dictionaryBoundsInt64(d.values, indexes)
	err.check()
	return min, max
}

func (d *floatDictionary) bounds(indexes []int32) (min, max float32) {
	min, max, err := dictionaryBoundsFloat32(d.values, indexes)
	err.check()
	return min, max
}

func (d *doubleDictionary) bounds(indexes []int32) (min, max float64) {
	min, max, err := dictionaryBoundsFloat64(d.values, indexes)
	err.check()
	return min, max
}

func (d *uint32Dictionary) bounds(indexes []int32) (min, max uint32) {
	min, max, err := dictionaryBoundsUint32(d.values, indexes)
	err.check()
	return min, max
}

func (d *uint64Dictionary) bounds(indexes []int32) (min, max uint64) {
	min, max, err := dictionaryBoundsUint64(d.values, indexes)
	err.check()
	return min, max
}

func (d *be128Dictionary) bounds(indexes []int32) (min, max *[16]byte) {
	min, max, err := dictionaryBoundsBE128(d.values, indexes)
	err.check()
	return min, max
}
