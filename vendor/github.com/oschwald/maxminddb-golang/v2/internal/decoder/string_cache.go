package decoder

import (
	"sync/atomic"
)

type cacheEntry struct {
	str    string
	offset uint
}

// stringCache holds two parallel 4096-slot arrays indexed by offset%4096.
// entries holds admitted strings; recentMisses records the last missing
// offset seen at each slot so we can admit only on the second consecutive
// miss for the same offset (see internAt).
//
// The arrays are intentionally separate rather than packed into a single
// [4096]struct{entry; recentMiss} layout. Packing halves slot density
// per cache line (4 vs 8) and measurably regresses BenchmarkStringCacheCold
// by ~8%: cold-sweep workloads benefit more from dense linear scanning of
// entries than from co-locating each entry with its rarely-touched miss
// counter.
type stringCache struct {
	entries      [4096]atomic.Pointer[cacheEntry]
	recentMisses [4096]atomic.Uint64
}

func newStringCache() *stringCache {
	return &stringCache{}
}

// internAt returns a string for the data at the given offset and size.
// Hot offsets are interned and the same backing string is returned on
// subsequent hits; cold offsets are returned freshly allocated on every
// call (see the admission rule below).
func (sc *stringCache) internAt(offset, size uint, data []byte) string {
	const (
		minCachedLen = 2   // single byte strings not worth caching
		maxCachedLen = 100 // reasonable upper bound for geographic strings
	)

	if size < minCachedLen || size > maxCachedLen {
		return string(data[offset : offset+size])
	}

	i := offset % uint(len(sc.entries))
	entry := &sc.entries[i]

	if cached := entry.Load(); cached != nil && cached.offset == offset {
		return cached.str
	}

	str := string(data[offset : offset+size])

	// Only admit strings that miss twice in the same slot. This keeps the
	// lock-free fast path for hot strings while avoiding heap churn for one-offs.
	// The +1 bias reserves 0 as the "no prior miss" sentinel so the initial
	// zero state of recentMisses[i] never spuriously matches a real offset of 0.
	admissionValue := uint64(offset) + 1
	if sc.recentMisses[i].Load() == admissionValue {
		entry.Store(&cacheEntry{
			str:    str,
			offset: offset,
		})
	} else {
		sc.recentMisses[i].Store(admissionValue)
	}

	return str
}
