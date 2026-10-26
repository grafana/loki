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

// KeyRange is the half-open key range [Start, End). A nil Start has no lower
// bound, and a nil End has no upper bound.
type KeyRange[K any] struct {
	Start, End *K

	// First is the smallest key in the range that the input held.
	First K

	// Size is the total size of the input keys in the range.
	Size uint64
}

// Contains reports whether key is in r.
func (r KeyRange[K]) Contains(key K, compare CompareFunc[K]) bool {
	if r.Start != nil && compare(key, *r.Start) < 0 {
		return false
	}
	return r.End == nil || compare(key, *r.End) < 0
}

// SplitKeyRanges splits the key space of keys into contiguous ranges of at
// most budget bytes each. The ranges cover the full key space without overlap,
// so every key, also one absent from keys, falls in exactly one range.
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
// SplitKeyRanges returns nil if keys is empty. It panics if budget is 0.
func SplitKeyRanges[K any](keys []KeySize[K], compare CompareFunc[K], budget uint64) []KeyRange[K] {
	if budget == 0 {
		panic(fmt.Sprintf("key range budget must be > 0, got %d", budget))
	}
	if len(keys) == 0 {
		return nil
	}

	sorted := slices.Clone(keys)
	slices.SortStableFunc(sorted, func(a, b KeySize[K]) int { return compare(a.Key, b.Key) })

	var ranges []KeyRange[K]
	current := KeyRange[K]{First: sorted[0].Key}
	for i := 0; i < len(sorted); {
		key := sorted[i].Key
		var size uint64
		for ; i < len(sorted) && compare(sorted[i].Key, key) == 0; i++ {
			size += sorted[i].Size
		}

		// A range over budget holds one oversized key, so this also closes
		// it before the next key.
		if current.Size > 0 && current.Size+size > budget {
			current.End = &key
			ranges = append(ranges, current)
			current = KeyRange[K]{Start: &key, First: key}
		}
		current.Size += size
	}

	if n := len(ranges); n > 0 {
		previous := ranges[n-1]
		if previous.Size <= budget && current.Size <= budget && previous.Size+current.Size <= budget+budget/10 {
			ranges[n-1].End = nil
			ranges[n-1].Size += current.Size
			return ranges
		}
	}
	return append(ranges, current)
}
