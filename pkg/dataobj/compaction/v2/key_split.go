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
// it. The range starts at its smallest input key.
type KeySplit[K any] struct {
	Range KeyRange[K]

	// Size is the total size of the input keys in the range.
	Size uint64

	// Oversized reports whether the range holds one key larger than the
	// budget of the split. That key is the start of the range, unless keys of
	// size 0 come before it. A last range that exceeds the budget after a
	// merge is not oversized.
	Oversized bool
}

// SplitKeyRanges splits the keys in keys into contiguous ranges of about
// budget bytes each. Each range starts at its smallest key, the last range
// ends at end, and each other range ends where the next one starts. Every key
// from the smallest input key up to end falls in exactly one range.
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
// SplitKeyRanges returns nil if keys is empty. It panics if budget is 0 or if
// a key is not below end.
func SplitKeyRanges[K any](keys []KeySize[K], compare CompareFunc[K], budget uint64, end K) []KeySplit[K] {
	if budget == 0 {
		panic(fmt.Sprintf("key range budget must be > 0, got %d", budget))
	}
	if len(keys) == 0 {
		return nil
	}

	sorted := slices.Clone(keys)
	slices.SortStableFunc(sorted, func(a, b KeySize[K]) int { return compare(a.Key, b.Key) })
	if compare(sorted[len(sorted)-1].Key, end) >= 0 {
		panic("key not below the end of the split")
	}

	// Each range starts at an input key and ends at a larger input key or at
	// end, so every range built here has its start below its end.
	var splits []KeySplit[K]
	current := KeySplit[K]{Range: KeyRange[K]{start: sorted[0].Key}}
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
		}
		if size > budget {
			current.Oversized = true
		}
		current.Size += size
	}
	current.Range.end = end

	if n := len(splits); n > 0 {
		previous := splits[n-1]
		if !previous.Oversized && !current.Oversized && previous.Size+current.Size <= budget+budget/10 {
			splits[n-1].Range.end = end
			splits[n-1].Size += current.Size
			return splits
		}
	}
	return append(splits, current)
}
