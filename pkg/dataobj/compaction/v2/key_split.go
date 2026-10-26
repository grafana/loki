package compactionv2

import (
	"fmt"
	"slices"
)

// KeySize is the uncompressed size of the data that one section holds for one
// key.
type KeySize[K any] struct {
	Key  K
	Size uint64
}

// KeyRange is the half-open key range [Start, End).
type KeyRange[K any] struct {
	Start, End K

	// First is the smallest input key in the range. It is Start if the range
	// holds no input key.
	First K

	// Size is the total size of the input keys in the range.
	Size uint64

	// Oversized reports whether the range holds one input key larger than
	// the budget of the split. The last range can exceed the budget without
	// being oversized.
	Oversized bool
}

// Contains reports whether key is in r.
func (r KeyRange[K]) Contains(key K, compare CompareFunc[K]) bool {
	return compare(key, r.Start) >= 0 && compare(key, r.End) < 0
}

// SplitKeyRanges splits [start, end) into contiguous ranges of at most budget
// bytes of keys each. The first range starts at start, the last range ends at
// end, and each other bound is shared by two neighbouring ranges. Every key in
// [start, end), also one absent from keys, falls in exactly one range.
//
// The same key can occur many times in keys, for example once per section.
// SplitKeyRanges adds up the sizes of equal keys and never splits a key across
// ranges.
//
// Two cases can exceed the budget:
//   - A key larger than budget gets a range of its own.
//   - The range before the last takes in the last range if their total stays
//     within 110% of budget. A small leftover then does not become a small
//     task. A range of one oversized key never takes part in this.
//
// SplitKeyRanges returns the single range [start, end) if keys is empty. It
// panics if budget is 0, if start is not below end, or if a key is outside
// [start, end).
func SplitKeyRanges[K any](keys []KeySize[K], compare CompareFunc[K], budget uint64, start, end K) []KeyRange[K] {
	if budget == 0 {
		panic(fmt.Sprintf("key range budget must be > 0, got %d", budget))
	}
	if compare(start, end) >= 0 {
		panic("key range start must be below its end")
	}

	sorted := slices.Clone(keys)
	slices.SortStableFunc(sorted, func(a, b KeySize[K]) int { return compare(a.Key, b.Key) })
	full := KeyRange[K]{Start: start, End: end}
	if len(sorted) > 0 && (!full.Contains(sorted[0].Key, compare) || !full.Contains(sorted[len(sorted)-1].Key, compare)) {
		panic("key outside the split range")
	}

	var ranges []KeyRange[K]
	current := KeyRange[K]{Start: start, First: start}
	currentHasKey := false
	for i := 0; i < len(sorted); {
		key := sorted[i].Key
		var size uint64
		for ; i < len(sorted) && compare(sorted[i].Key, key) == 0; i++ {
			size += sorted[i].Size
		}

		// A range over budget holds one oversized key, so this also closes
		// it before the next key.
		if current.Size > 0 && current.Size+size > budget {
			current.End = key
			ranges = append(ranges, current)
			current = KeyRange[K]{Start: key}
			currentHasKey = false
		}
		if !currentHasKey {
			current.First = key
			current.Oversized = size > budget
			currentHasKey = true
		}
		current.Size += size
	}
	current.End = end

	if n := len(ranges); n > 0 {
		previous := ranges[n-1]
		if !previous.Oversized && !current.Oversized && previous.Size+current.Size <= budget+budget/10 {
			ranges[n-1].End = end
			ranges[n-1].Size += current.Size
			return ranges
		}
	}
	return append(ranges, current)
}
