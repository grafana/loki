package index

import (
	"math"
	"sort"
)

// (SeriesRef, Fingerprint) tuples
type FingerprintOffsets [][2]uint64

func (xs FingerprintOffsets) Range(fpFilter FingerprintFilter) (minOffset, maxOffset uint64) {
	from, through := fpFilter.GetFromThrough()
	lower := sort.Search(len(xs), func(i int) bool {
		return xs[i][1] >= uint64(from)
	})

	if lower < len(xs) && lower > 0 {
		// If lower is the first series offset
		// to exist in this shard, we must also check
		// any offsets since the previous sample as well
		minOffset = xs[lower-1][0]
	}

	upper := sort.Search(len(xs), func(i int) bool {
		return xs[i][1] >= uint64(through)
	})

	// If there are no sampled fingerprints greater than this shard,
	// we must check to the end of TSDB series offsets.
	if upper == len(xs) {
		maxOffset = math.MaxUint64
	} else {
		maxOffset = xs[upper][0]
	}

	return
}
