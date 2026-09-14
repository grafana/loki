package decoder

import (
	"sync/atomic"
)

type cacheEntry struct {
	str    string
	offset uint
}

const stringCacheSlots = 4096

// stringCache holds two parallel arrays indexed by string control-record offset.
// entries holds admitted strings; recentMisses records the last missing
// offset seen at each slot so we can admit only on the second consecutive
// miss for the same offset (see internAt).
//
// The arrays are intentionally separate rather than packed into a single
// [4096]struct{entry; recentMiss} layout. This keeps the frequently scanned
// entries denser in cache lines while misses use a separate counter array.
type stringCache struct {
	entries      [stringCacheSlots]atomic.Pointer[cacheEntry]
	recentMisses [stringCacheSlots]atomic.Uint64
}

func newStringCache() *stringCache {
	return &stringCache{}
}

// internAt returns a string for value, keyed by the offset of its MMDB string
// control record. A control-record offset uniquely identifies both the payload
// offset and its encoded length, including for overlapping encodings.
//
// Hot offsets are interned and the same backing string is returned on
// subsequent hits; cold offsets are returned freshly allocated on every
// call (see the admission rule below).
func (sc *stringCache) internAt(offset uint, value []byte) string {
	const (
		minCachedLen = 2   // single byte strings not worth caching
		maxCachedLen = 100 // reasonable upper bound for geographic strings
	)

	size := uint(len(value))
	if size < minCachedLen || size > maxCachedLen {
		return string(value)
	}

	const mask = stringCacheSlots - 1
	primary := offset & mask
	entry := &sc.entries[primary]

	if cached := entry.Load(); cached != nil && cached.offset == offset {
		return cached.str
	}
	alternate := stringCacheAlternateIndex(offset, primary)
	if cached := sc.entries[alternate].Load(); cached != nil && cached.offset == offset {
		return cached.str
	}

	str := string(value)

	// Only admit strings that miss twice in the same slot. This keeps the
	// lock-free fast path for hot strings while avoiding heap churn for one-offs.
	// The +1 bias reserves 0 as the "no prior miss" sentinel so the initial
	// zero state of recentMisses[i] never spuriously matches a real offset of 0.
	admissionValue := uint64(offset) + 1
	if sc.recentMisses[alternate].Load() == admissionValue {
		if entry.Load() != nil {
			entry = &sc.entries[alternate]
		}
		entry.Store(&cacheEntry{
			str:    str,
			offset: offset,
		})
	} else {
		sc.recentMisses[alternate].Store(admissionValue)
	}

	return str
}

func stringCacheAlternateIndex(offset, primary uint) uint {
	const mask = stringCacheSlots - 1
	return (primary + ((offset>>12)*0x9e37 | 1)) & mask
}
