package compactionv2

import (
	"fmt"
	"slices"
)

// KeySize is the uncompressed size of the data that one section holds for one
// key.
type KeySize[K any] struct {
	Key              K
	UncompressedSize uint64
}

// KeyRange is the half-open key range [start, end) under its compare
// function. Its start is always below its end.
type KeyRange[K any] struct {
	start, end K
	compare    CompareFunc[K]
}

// NewKeyRange returns the range [start, end). It returns an error if start is
// not below end under compare.
func NewKeyRange[K any](start, end K, compare CompareFunc[K]) (KeyRange[K], error) {
	if compare(start, end) >= 0 {
		return KeyRange[K]{}, fmt.Errorf("key range start %v is not below its end %v", start, end)
	}
	return KeyRange[K]{start: start, end: end, compare: compare}, nil
}

// Start returns the smallest key in r.
func (r KeyRange[K]) Start() K { return r.start }

// End returns the smallest key above r.
func (r KeyRange[K]) End() K { return r.end }

// Contains reports whether key is in r.
func (r KeyRange[K]) Contains(key K) bool {
	return r.compare(key, r.start) >= 0 && r.compare(key, r.end) < 0
}

// KeySplit is one range of a split by [SplitKeyRanges] and the input keys in
// it. The range starts at its smallest input key.
type KeySplit[K any] struct {
	Range KeyRange[K]

	// UncompressedSize is the total uncompressed size of the input keys in
	// the range.
	UncompressedSize uint64

	// Oversized reports whether the range holds one key larger than the
	// budget of the split. That key is the start of the range, unless keys of
	// size 0 come before it.
	//
	// An oversized range still excludes its end. Its end is the next input key
	// or the end of the split, so it never equals the start.
	//
	// A last range that exceeds the budget after a merge is not oversized.
	Oversized bool
}

// SplitKeyRanges splits keys into contiguous half-open ranges of about budget
// bytes each. Each range starts at its smallest key and excludes its end. The
// last range ends at end, so end itself is in no range. Each other range ends
// where the next one starts. Every key from the smallest input key up to, but
// not including, end falls in exactly one range.
//
// The same key can occur many times in keys, for example once per section.
// SplitKeyRanges adds up the sizes of equal keys and never splits a key across
// ranges.
//
// A range exceeds the budget in two cases:
//   - A key larger than budget gets a range of its own. Like every range, it
//     starts at that key and excludes its end, the next input key or end.
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
	if last := sorted[len(sorted)-1].Key; compare(last, end) >= 0 {
		panic(fmt.Sprintf("key %v is not below the end %v of the split", last, end))
	}

	// Each range starts at an input key and ends at a larger input key or at
	// end, so NewKeyRange never fails here.
	mustNewRange := func(start, end K) KeyRange[K] {
		r, err := NewKeyRange(start, end, compare)
		if err != nil {
			panic(fmt.Sprintf("split key range: %v", err))
		}
		return r
	}

	var (
		splits    []KeySplit[K]
		start     = sorted[0].Key
		size      uint64
		oversized bool
	)
	for i := 0; i < len(sorted); {
		key := sorted[i].Key
		var keySize uint64
		for ; i < len(sorted) && compare(sorted[i].Key, key) == 0; i++ {
			keySize += sorted[i].UncompressedSize
		}

		// Close the current range before key if key does not fit. A range
		// over budget holds one oversized key, so this check also closes it.
		if size > 0 && size+keySize > budget {
			splits = append(splits, KeySplit[K]{Range: mustNewRange(start, key), UncompressedSize: size, Oversized: oversized})
			start, size, oversized = key, 0, false
		}
		oversized = oversized || keySize > budget
		size += keySize
	}

	if n := len(splits); n > 0 {
		previous := splits[n-1]
		if !previous.Oversized && !oversized && previous.UncompressedSize+size <= budget+budget/10 {
			splits[n-1] = KeySplit[K]{Range: mustNewRange(previous.Range.Start(), end), UncompressedSize: previous.UncompressedSize + size}
			return splits
		}
	}
	return append(splits, KeySplit[K]{Range: mustNewRange(start, end), UncompressedSize: size, Oversized: oversized})
}
