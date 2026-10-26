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

// KeyRange is the half-open key range [start, end). Its start is always below
// its end under the compare function that built it.
type KeyRange[K any] struct {
	start, end K
}

// NewKeyRange returns the range [start, end). It returns an error if start is
// not below end under compare.
func NewKeyRange[K any](start, end K, compare CompareFunc[K]) (KeyRange[K], error) {
	if compare(start, end) >= 0 {
		return KeyRange[K]{}, fmt.Errorf("key range start %v is not below its end %v", start, end)
	}
	return KeyRange[K]{start: start, end: end}, nil
}

// Start returns the smallest key in r.
func (r KeyRange[K]) Start() K { return r.start }

// End returns the smallest key above r.
func (r KeyRange[K]) End() K { return r.end }

// Contains reports whether key is in r. compare must be the function that
// built r.
func (r KeyRange[K]) Contains(key K, compare CompareFunc[K]) bool {
	return compare(key, r.start) >= 0 && compare(key, r.end) < 0
}

// KeySplit is one range of a split by [SplitKeyRanges] and the input keys in
// it.
type KeySplit[K any] struct {
	Range KeyRange[K]

	// First is the smallest input key in the range. It is the start of the
	// range if the range holds no input key.
	First K

	// Size is the total size of the input keys in the range.
	Size uint64

	// Oversized reports whether the range holds one input key larger than
	// the budget of the split. The last range can exceed the budget without
	// being oversized.
	Oversized bool
}

// SplitKeyRanges splits full into contiguous ranges of about budget bytes of
// keys each. The first range starts at the start of full, the last range ends
// at its end, and each other bound is shared by two neighbouring ranges. Every
// key in full falls in exactly one range, including keys absent from keys.
//
// The same key can occur many times in keys, for example once per section.
// SplitKeyRanges adds up the sizes of equal keys and never splits a key across
// ranges.
//
// A range exceeds the budget in two cases:
//   - A key larger than budget gets a range of its own.
//   - The last range merges into the range before it if their total stays
//     within 110% of budget. This avoids a small last range. A range that
//     holds one oversized key never merges.
//
// SplitKeyRanges returns full alone if keys is empty. It panics if budget is
// 0 or if a key is outside full.
func SplitKeyRanges[K any](keys []KeySize[K], compare CompareFunc[K], budget uint64, full KeyRange[K]) []KeySplit[K] {
	if budget == 0 {
		panic(fmt.Sprintf("key range budget must be > 0, got %d", budget))
	}

	sorted := slices.Clone(keys)
	slices.SortStableFunc(sorted, func(a, b KeySize[K]) int { return compare(a.Key, b.Key) })
	if len(sorted) > 0 && (!full.Contains(sorted[0].Key, compare) || !full.Contains(sorted[len(sorted)-1].Key, compare)) {
		panic("key outside the split range")
	}

	// Cuts happen only at input keys above the start of the current range,
	// so every range built here has its start below its end.
	var splits []KeySplit[K]
	current := KeySplit[K]{Range: KeyRange[K]{start: full.start}, First: full.start}
	currentHasKey := false
	for i := 0; i < len(sorted); {
		key := sorted[i].Key
		var size uint64
		for ; i < len(sorted) && compare(sorted[i].Key, key) == 0; i++ {
			size += sorted[i].Size
		}

		// Close the current range before key if key does not fit. A range
		// over budget holds one oversized key, so this check also closes it.
		if current.Size > 0 && current.Size+size > budget {
			current.Range.end = key
			splits = append(splits, current)
			current = KeySplit[K]{Range: KeyRange[K]{start: key}}
			currentHasKey = false
		}
		if !currentHasKey {
			current.First = key
			current.Oversized = size > budget
			currentHasKey = true
		}
		current.Size += size
	}
	current.Range.end = full.end

	if n := len(splits); n > 0 {
		previous := splits[n-1]
		if !previous.Oversized && !current.Oversized && previous.Size+current.Size <= budget+budget/10 {
			splits[n-1].Range.end = full.end
			splits[n-1].Size += current.Size
			return splits
		}
	}
	return append(splits, current)
}
