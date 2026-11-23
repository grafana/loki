package dataset

import (
	"encoding/binary"
	"fmt"
	"iter"

	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A selection is a set of rows of a record batch of n rows, as a bitmap. A set
// bit selects the row.
//
// A selection with a bitmap of length zero selects every row. This follows the
// selection vector convention of package compute, and it saves building a bitmap
// that selects every row. Any other bitmap has n bits.
//
// A method that returns a selection can return its receiver or an argument. So
// a caller must not modify a selection that it received, or that it already
// returned.
type selection struct {
	bits memory.Bitmap
	n    int
}

// newSelectionAllRows returns a selection of n rows that selects every row.
func newSelectionAllRows(n int) selection { return selection{n: n} }

// newSelection returns a selection of n rows that all have the given value. If
// value is true, the selection has no bitmap.
func newSelection(alloc *memory.Allocator, n int, value bool) selection {
	if value {
		return newSelectionAllRows(n)
	}

	bits := memory.NewBitmap(alloc, n)
	bits.AppendCount(false, n)
	return selection{bits: bits, n: n}
}

// isAllRows reports whether s is the selection of every row that has no bitmap.
// It returns false for a bitmap that has every bit set.
func (s selection) isAllRows() bool { return s.bits.Len() == 0 }

// set selects row i. Use it only to build a selection that nobody else holds.
func (s *selection) set(i int) { s.bits.Set(i, true) }

// count returns how many rows s selects.
func (s selection) count() int {
	if s.isAllRows() {
		return s.n
	}
	return s.bits.SetCount()
}

// empty reports whether s selects no row.
//
// It stops at the first set bit, so it costs less than [selection.count] when s
// selects a row. When s selects no row, it reads every byte, like
// [selection.count].
func (s selection) empty() bool {
	if s.isAllRows() {
		return s.n == 0
	}

	// The bits of s are bits [off, off+length) of data. The bytes at both ends can
	// hold bits outside s, so mask them.
	var (
		data, off   = s.bits.Bytes()
		end         = off + s.bits.Len()
		first, last = off / 8, (end - 1) / 8
		firstMask   = byte(0xFF) << (off % 8)
		lastMask    = byte(0xFF) >> (7 - (end-1)%8)
	)
	if first == last {
		return data[first]&firstMask&lastMask == 0
	}
	if data[first]&firstMask != 0 || data[last]&lastMask != 0 {
		return false
	}

	middle := data[first+1 : last]
	for ; len(middle) >= 8; middle = middle[8:] {
		if binary.LittleEndian.Uint64(middle) != 0 {
			return false
		}
	}
	for _, b := range middle {
		if b != 0 {
			return false
		}
	}
	return true
}

// forEach calls fn with the index of each row that s selects, in ascending
// order.
func (s selection) forEach(fn func(i int)) {
	if s.isAllRows() {
		for i := range s.n {
			fn(i)
		}
		return
	}
	for i := range s.bits.IterValues(true) {
		fn(i)
	}
}

// unselectedRuns returns the runs of consecutive rows that s does not select, in
// ascending order. Each run is the range [start, end) of row indexes. The
// selection of every row has no run.
func (s selection) unselectedRuns() iter.Seq2[int, int] {
	return func(yield func(start, end int) bool) {
		var (
			start = -1 // Start of the current run, or -1 before the first run.
			next  int  // Index after the last row of the current run.
		)
		for i := range s.bits.IterValues(false) {
			if start >= 0 && i == next {
				next++
				continue
			}
			if start >= 0 && !yield(start, next) {
				return
			}
			start, next = i, i+1
		}
		if start >= 0 {
			yield(start, next)
		}
	}
}

// slice returns the rows [i, j) of s as a selection of j-i rows.
func (s selection) slice(i, j int) selection {
	if s.isAllRows() {
		return newSelectionAllRows(j - i)
	}
	return selection{bits: *s.bits.Slice(i, j), n: j - i}
}

// filterArray returns the elements of arr that s selects, in memory from
// alloc. It copies arr even if s selects every element.
func (s selection) filterArray(alloc *memory.Allocator, arr columnar.Array) (columnar.Array, error) {
	// compute.Filter returns its input when s selects every element. The result
	// must not share memory with arr, which can be scratch memory of the input
	// record batch.
	if s.isAllRows() || s.count() == arr.Len() {
		return columnar.CloneArray(alloc, arr)
	}

	filtered, err := compute.Filter(alloc, arr, s.bits)
	if err != nil {
		return nil, err
	}
	return columnar.AsArray(filtered)
}

// checkRows panics if s and other are selections of a different number of rows.
func (s selection) checkRows(other selection) {
	if s.n != other.n {
		panic(fmt.Sprintf("dataset: selections of %d rows and %d rows", s.n, other.n))
	}
}

// and returns the rows that both s and other select.
func (s selection) and(alloc *memory.Allocator, other selection) selection {
	s.checkRows(other)
	switch {
	case s.isAllRows():
		return other
	case other.isAllRows():
		return s
	}

	out := memory.NewBitmap(alloc, s.n)
	out.Resize(s.n)

	var (
		sBytes, sOff     = s.bits.Bytes()
		otherBytes, oOff = other.bits.Bytes()
		outBytes, outOff = out.Bytes()
	)
	bitutil.BitmapAnd(sBytes, otherBytes, int64(sOff), int64(oOff), outBytes, int64(outOff), int64(s.n))
	return selection{bits: out, n: s.n}
}

// or returns the rows that s or other select.
func (s selection) or(alloc *memory.Allocator, other selection) selection {
	s.checkRows(other)
	switch {
	case s.isAllRows():
		return s
	case other.isAllRows():
		return other
	}

	out := memory.NewBitmap(alloc, s.n)
	out.Resize(s.n)

	var (
		sBytes, sOff     = s.bits.Bytes()
		otherBytes, oOff = other.bits.Bytes()
		outBytes, outOff = out.Bytes()
	)
	bitutil.BitmapOr(sBytes, otherBytes, int64(sOff), int64(oOff), outBytes, int64(outOff), int64(s.n))
	return selection{bits: out, n: s.n}
}

// subtract returns the rows that s selects and other does not.
func (s selection) subtract(alloc *memory.Allocator, other selection) selection {
	s.checkRows(other)
	if other.isAllRows() {
		return newSelection(alloc, s.n, false)
	}

	out := memory.NewBitmap(alloc, s.n)
	out.Resize(s.n)
	outBytes, outOff := out.Bytes()
	otherBytes, oOff := other.bits.Bytes()

	if s.isAllRows() {
		bitutil.InvertBitmap(otherBytes, oOff, s.n, outBytes, outOff)
		return selection{bits: out, n: s.n}
	}

	sBytes, sOff := s.bits.Bytes()
	bitutil.BitmapAndNot(sBytes, otherBytes, int64(sOff), int64(oOff), outBytes, int64(outOff), int64(s.n))
	return selection{bits: out, n: s.n}
}

// selectionFromComparison converts the result of a comparison kernel into a
// selection. A row where the kernel input was null passes if nullPasses is true.
// The result selects only rows that sel selects.
func selectionFromComparison(alloc *memory.Allocator, res columnar.Datum, nullPasses bool, sel selection) (selection, error) {
	arr, ok := res.(*columnar.Bool)
	if !ok {
		return selection{}, fmt.Errorf("comparison kernel returned %T, want a boolean array", res)
	} else if arr.Len() != sel.n {
		return selection{}, fmt.Errorf("comparison kernel returned %d rows, want %d", arr.Len(), sel.n)
	}

	n := sel.n
	values := arr.Values()

	if arr.Nulls() > 0 {
		validity := arr.Validity()

		masked := memory.NewBitmap(alloc, n)
		masked.Resize(n)

		var (
			valuesBytes, valuesOff     = values.Bytes()
			validityBytes, validityOff = validity.Bytes()
			maskedBytes, maskedOff     = masked.Bytes()
		)
		bitutil.BitmapAnd(valuesBytes, validityBytes, int64(valuesOff), int64(validityOff), maskedBytes, int64(maskedOff), int64(n))

		if nullPasses {
			inverted := memory.NewBitmap(alloc, n)
			inverted.Resize(n)
			invertedBytes, invertedOff := inverted.Bytes()
			bitutil.InvertBitmap(validityBytes, validityOff, n, invertedBytes, invertedOff)
			bitutil.BitmapOr(maskedBytes, invertedBytes, int64(maskedOff), int64(invertedOff), maskedBytes, int64(maskedOff), int64(n))
		}
		values = masked
	}

	// The kernel leaves the values of rows outside sel undefined, so intersect
	// the result with sel.
	return selection{bits: values, n: n}.and(alloc, sel), nil
}
