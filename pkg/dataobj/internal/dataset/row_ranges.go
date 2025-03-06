package dataset

import (
	"cmp"
	"slices"
	"sort"
)

// rowRanges tracks a set of row ranges that are "valid."
type rowRanges []rowRange

// Add adds the given range to the set of rowRanges.
func (rr *rowRanges) Add(r rowRange) {
	ranges := *rr
	defer func() { *rr = ranges }()

	if len(ranges) == 0 {
		ranges = append(ranges, r)
		return
	}

	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].Start >= r.Start
	})
	switch {
	case i < len(ranges) && ranges[i].Start == r.Start:
		// An existing range starts at the same row. Rather than adding a new
		// range, we'll expand the existing one. (This may cause overlaps; see the
		// loop below.)
		ranges[i].End = max(ranges[i].End, r.End)

	case i > 0 && ranges[i-1].End >= r.Start:
		// The new range overlaps with the previous range. Similar to the case
		// above, we'll expand the existing one.
		ranges[i-1].End = max(ranges[i-1].End, r.End)

	default:
		// Fall back to just adding the new range; this may still cause overlaps,
		// but we'll fix them below.
		ranges = slices.Insert(ranges, i, r)
	}

	// It's possible that our new range overlaps with the next range. If so, we
	// fix the ranges by splitting the midpoint with the overlapped range.
	//
	// Fixing the ranges in this way may then cause an overlap between the next
	// two pairs of ranges, so we check for overlaps until we find the first pair
	// of non-overlapping ranges.
	//
	// This operation could also be done by merging two ranges, but merging two
	// ranges would be expensive for a slice, as it would require removing i+1
	// and copying over the rest of the slice back one element.
	for j := i; j < len(ranges)-1; j++ {
		// Two ranges a and b only overlap if range a ends after range b starts:
		// a.End >= b.Start.
		if ranges[j].End < ranges[j+1].Start {
			break // No overlap.
		}

		mid := (ranges[j].Start + ranges[j+1].End) / 2
		ranges[j].End = mid - 1
		ranges[j+1].Start = mid
		ranges[j+1].End = max(ranges[j+1].End, r.End)
	}
}

// Range returns the range of rows that includes row, or false if no such range
// exists.
func (rr *rowRanges) Range(row uint64) (rowRange, bool) {
	ranges := *rr
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End >= row
	})
	if i < len(ranges) && row >= ranges[i].Start {
		return ranges[i], true
	}
	return rowRange{}, false
}

// Includes returns true if the given row is included in any of the ranges.
func (rr *rowRanges) Includes(row uint64) bool {
	_, ok := rr.Range(row)
	return ok
}

// IncludesRange returns true if every row in the given range overlap with
// ranges in rr.
func (rr *rowRanges) IncludesRange(other rowRange) bool {
	// other may be included in rr but split across multiple ranges, since one
	// continugous range can be split up into multiple elements in rr.
	//
	// rr   :  [-----][------][------]   [------]
	// other:    [-------]
	//
	// To handle this, we build the largest continguous range in rr starting from
	// the first range that other overlaps with. If other is fully included in
	// that range, then IncludesRange is true.
	//
	// range:  [--------------------]
	// other:    [-------]
	//
	// If there are gaps, then the continugous range would be cut short, denoting
	// that other is not fully included:
	//
	// rr   :  [-----][---]  [------]   [------]
	// other:    [----------]
	//
	// range:  [----------]
	// other:    [----------]
	ranges := *rr
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End >= other.Start
	})
	if i == len(ranges) {
		return false
	}

	// Build the largest continugous range from i that we can; two ranges a and b
	// are contiguous if b starts at the row after a ends.
	checkRange := rowRange{Start: ranges[i].Start, End: ranges[i].End}
	for ; i < len(ranges) && ranges[i].Start <= checkRange.End+1; i++ {
		checkRange.End = ranges[i].End
	}
	return checkRange.ContainsRange(other)
}

// Overlaps returns true if any rows in the given range overlap with any range
// in rr.
func (rr *rowRanges) Overlaps(other rowRange) bool {
	ranges := *rr
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End >= other.Start
	})
	for ; i < len(ranges) && ranges[i].Start <= other.End; i++ {
		if ranges[i].Overlaps(other) {
			return true
		}
	}
	return false
}

// Next returns the next included row after the given row number, or false if
// row is beyond the last row.
func (rr *rowRanges) Next(row uint64) (uint64, bool) {
	nextRow := row + 1

	ranges := *rr
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End >= nextRow
	})
	if i == len(ranges) {
		return 0, false
	}

	if nextRow < ranges[i].Start {
		return ranges[i].Start, true
	}
	return nextRow, true
}

// intersectRanges appends the intersection of two sets of ranges into dst,
// returning the result.
//
// The memory of dst must not overlap with a or b.
func intersectRanges(dst rowRanges, a, b rowRanges) rowRanges {
	dst = dst[:0]

	if len(a) == 0 || len(b) == 0 {
		return dst
	}

	// Our intersection works by doing a merge-style sort.
	//
	// Since both ranges are sorted, we can walk through them in parallel,
	// checking for overlaps between the current ranges. Whenever we find an
	// overlap, we overwrite rr with the overlapping range. This gives us a
	// complexity of O(n+m), which is O(len(ranges)+len(other)).

	for i, j := 0, 0; i < len(a) && j < len(b); {
		// Two ranges overlap if the start of the range is less than or equal to the
		// end of the merged range.
		//
		// For example, the ranges [10, 20] and [100, 200] don't overlap because
		//
		//   start = max(10, 100) = 100
		//   end   = min(20, 200) = 20
		//
		// But the ranges [100, 200] and [150, 250] do overlap:
		//
		//   start = max(100, 150) = 150
		//   end   = min(200, 250) = 200
		var (
			start = max(a[i].Start, b[j].Start)
			end   = min(a[i].End, b[j].End)
		)
		if start <= end {
			dst = append(dst, rowRange{Start: start, End: end})
		}

		// We only move the pointer of the range that ends first, because:
		//
		// * If other[j] ends first, it can't possibly overlap with ranges[i]
		//   anymore, but other[j+1] might.
		//
		// * If ranges[i] ends first, it can't possibly overlap with other[j]
		//   anymore, but ranges[i+1] might.
		//
		// This ensures we don't miss any potential overlaps while still
		// maintaining O(n+m) complexity.
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}

	return dst
}

// unionRanges appends the union of two sets of ranges into dst,
// returning the result.
//
// The memory of dst must not overlap with a or b.
func unionRanges(dst rowRanges, a, b rowRanges) rowRanges {
	dst = dst[:0]

	// Simple cases: if either range is empty, the union is the other range.
	switch {
	case len(a) == 0:
		return append(dst, b...)
	case len(b) == 0:
		return append(dst, a...)
	}

	// We do our union by adding everything from a and b together, sorting
	// the merged range, and then fixing any overlapping ranges.
	dst = slices.Grow(dst, len(a)+len(b))
	dst = append(dst, a...)
	dst = append(dst, b...)

	slices.SortFunc(dst, func(a, b rowRange) int {
		return cmp.Compare(a.Start, b.Start)
	})

	// We merge ranges in-place by walking through the slice and merging any
	// overlapping ranges.
	//
	// We use the entirety of len(dst) as scratch space, so we keep track of the
	// write index separately and then truncate the final slice.

	writeIdx := 1 // dst[0] is always included (but may be modified).
	for i := 1; i < len(dst); i++ {
		// If the current range overlaps with the previous range, we can merge them
		// together.
		//
		// To reduce the total length of the slice, we also check for adjacent
		// ranges (via End+1) so that [1, 5] and [6, 10] are merged into a single
		// range [1, 10].
		//
		// If there's no overlap, we write out the range unmodified.
		if dst[i].Start <= dst[writeIdx-1].End+1 {
			dst[writeIdx-1].End = max(dst[writeIdx-1].End, dst[i].End)
		} else {
			dst[writeIdx] = dst[i]
			writeIdx++
		}
	}

	return dst[:writeIdx]
}
