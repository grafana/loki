//go:build purego || !amd64

package parquet

import (
	"unsafe"

	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

func (d *int32Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*int32)(rows.Index(i)) = d.index(j)
	}
}

func (d *int64Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*int64)(rows.Index(i)) = d.index(j)
	}
}

func (d *floatDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*float32)(rows.Index(i)) = d.index(j)
	}
}

func (d *doubleDictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*float64)(rows.Index(i)) = d.index(j)
	}
}

func (d *byteArrayDictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*string)(rows.Index(i)) = unsafecast.String(d.index(int(j)))
	}
}

func (d *fixedLenByteArrayDictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*string)(rows.Index(i)) = unsafecast.String(d.index(j))
	}
}

func (d *uint32Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*uint32)(rows.Index(i)) = d.index(j)
	}
}

func (d *uint64Dictionary) lookup(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(*uint64)(rows.Index(i)) = d.index(j)
	}
}

func (d *be128Dictionary) lookupString(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	s := "0123456789ABCDEF"
	for i, j := range indexes {
		*(**[16]byte)(unsafe.Pointer(&s)) = d.index(j)
		*(*string)(rows.Index(i)) = s
	}
}

func (d *be128Dictionary) lookupPointer(indexes []int32, rows sparse.Array) {
	checkLookupIndexBounds(indexes, rows)
	for i, j := range indexes {
		*(**[16]byte)(rows.Index(i)) = d.index(j)
	}
}

func (d *int32Dictionary) bounds(indexes []int32) (min, max int32) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *int64Dictionary) bounds(indexes []int32) (min, max int64) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *floatDictionary) bounds(indexes []int32) (min, max float32) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *doubleDictionary) bounds(indexes []int32) (min, max float64) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *uint32Dictionary) bounds(indexes []int32) (min, max uint32) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *uint64Dictionary) bounds(indexes []int32) (min, max uint64) {
	min = d.index(indexes[0])
	max = min

	for _, i := range indexes[1:] {
		value := d.index(i)
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}

	return min, max
}

func (d *be128Dictionary) bounds(indexes []int32) (min, max *[16]byte) {
	values := [64]*[16]byte{}
	min = d.index(indexes[0])
	max = min

	for i := 1; i < len(indexes); i += len(values) {
		n := len(indexes) - i
		if n > len(values) {
			n = len(values)
		}
		j := i + n
		d.lookupPointer(indexes[i:j:j], makeArrayBE128(values[:n:n]))

		for _, value := range values[:n:n] {
			switch {
			case lessBE128(value, min):
				min = value
			case lessBE128(max, value):
				max = value
			}
		}
	}

	return min, max
}
