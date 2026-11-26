package decoder

import (
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
)

type cacheEntry struct {
	str    string
	offset uint
}

const (
	stringCacheBucketBits  = 10
	stringCacheBuckets     = 1 << stringCacheBucketBits
	stringCacheBucketSlots = 7

	// Group nearby offsets so strings in one record use nearby cache lines.
	// Non-overlapping string encodings fit within the seven home slots.
	// Overlapping encodings remain valid and can use other slots.
	stringCacheWindowBits = 4
	stringCacheWindowMask = 1<<stringCacheWindowBits - 1
	stringCachePassShift  = stringCacheWindowBits + stringCacheBucketBits

	// Omit the low bit to group nearby non-overlapping strings.
	// Overlapping strings may share a miss-history slot.
	stringCacheMissShift = 1

	// This miss table and the buckets make the table 72 KiB on 64-bit systems.
	stringCacheMisses = 1008

	// Each slot has a control byte; the final byte holds reference bits.
	// A zero control byte marks an empty slot.
	stringCacheOccupied        = 0x80
	stringCacheSlotLowBits     = 0x00_01_01_01_01_01_01_01
	stringCacheSlotHighBits    = 0x00_80_80_80_80_80_80_80
	stringCacheReferencedShift = 8 * stringCacheBucketSlots
	stringCacheAllSlots        = 1<<stringCacheBucketSlots - 1
	stringCacheReferencedBits  = stringCacheAllSlots << stringCacheReferencedShift
	// The unused top bit serializes writers; readers verify entry offsets.
	stringCacheWriting = uint64(1) << 63
)

// cacheBucket fits its control word and entry pointers in one 64-byte line.
type cacheBucket struct {
	entries [stringCacheBucketSlots]atomic.Pointer[cacheEntry]
	control atomic.Uint64
}

// stringCache shares lazy initialization across copies of a DataDecoder.
type stringCache struct {
	table      atomic.Pointer[stringCacheTable]
	initialize sync.Once
}

type stringCacheTable struct {
	buckets [stringCacheBuckets]cacheBucket

	// Keep this read-mostly flag off the frequently written miss table's
	// 128-byte cache lines.
	searching atomic.Bool
	_         [124]byte

	// Each word keeps two misses so neighboring strings can both be admitted.
	recentMisses [stringCacheMisses]atomic.Uint64
}

func newStringCache() *stringCache {
	return &stringCache{}
}

// internAt returns the cached string for a control-record offset or converts
// value to a new string. The offset identifies both payload and encoded size.
func (sc *stringCache) internAt(offset uint, value []byte) string {
	const (
		minCachedLen = 2   // single byte strings not worth caching
		maxCachedLen = 100 // reasonable upper bound for geographic strings
	)

	size := uint(len(value))
	if size < minCachedLen || size > maxCachedLen {
		return string(value)
	}

	table := sc.table.Load()
	if table == nil {
		table = sc.initTable()
	}
	bucket := table.bucket(offset)

	// Before displacement or aging, every entry is at home and a miss needs
	// no bucket scan. Aging enables scans so hits update reference bits.
	if !table.searching.Load() {
		cached := bucket.entries[stringCacheHomeSlot(offset)].Load()
		if cached != nil && cached.offset == offset {
			recordStringCacheHit()
			return cached.str
		}
		return table.miss(bucket, offset, value)
	}

	// Keep candidate scans at their call sites to avoid a helper call on cache hits.
	control := bucket.control.Load()
	matches := stringCacheMatches(control, stringCacheControlByte(offset))
	for matches != 0 {
		slot := uint(bits.TrailingZeros64(matches)) / 8
		if cached := bucket.entries[slot].Load(); cached != nil && cached.offset == offset {
			// Mark the first hit after aging; later hits avoid shared writes.
			referenced := uint64(1) << (stringCacheReferencedShift + slot)
			if control&referenced == 0 {
				bucket.control.CompareAndSwap(control, control|referenced)
			}
			recordStringCacheHit()
			return cached.str
		}
		matches &= matches - 1
	}

	return table.miss(bucket, offset, value)
}

// initTable prevents concurrent first reads from allocating duplicate tables.
func (sc *stringCache) initTable() *stringCacheTable {
	sc.initialize.Do(func() { sc.table.Store(new(stringCacheTable)) })
	return sc.table.Load()
}

func (table *stringCacheTable) bucket(offset uint) *cacheBucket {
	return &table.buckets[(offset>>stringCacheWindowBits)&(stringCacheBuckets-1)]
}

// miss admits a string only after two misses, avoiding entries for one-off values.
// A late duplicate reuses the published string without allocating another copy.
//
//go:noinline
func (table *stringCacheTable) miss(bucket *cacheBucket, offset uint, value []byte) string {
	recordStringCacheMiss()

	recent := table.recentMiss(offset)
	admissionValue := stringCacheAdmissionValue(offset)
	if admissionValue == 0 {
		return string(value)
	}
	recorded := recent.Load()
	remaining, missed := stringCacheTakeMiss(recorded, admissionValue)
	if !missed {
		recent.Store(recorded<<32 | uint64(admissionValue))
		return string(value)
	}

	home := stringCacheHomeSlot(offset)
	control := bucket.control.Load()
	// Reuse a concurrently published entry before competing for the writer bit.
	for candidates := stringCacheMatches(control, stringCacheControlByte(offset)); candidates != 0; candidates &= candidates - 1 {
		slot := uint(bits.TrailingZeros64(candidates)) / 8
		if cached := bucket.entries[slot].Load(); cached != nil && cached.offset == offset {
			referenced := uint64(1) << (stringCacheReferencedShift + slot)
			if control&referenced == 0 {
				bucket.control.CompareAndSwap(control, control|referenced)
			}
			recent.CompareAndSwap(recorded, remaining)
			return cached.str
		}
	}
	// Serialize publishers so a delayed writer cannot overwrite a newer entry.
	if control&stringCacheWriting != 0 ||
		!bucket.control.CompareAndSwap(control, control|stringCacheWriting) {
		return string(value)
	}
	// A publisher can replace an entry and restore the same control word (ABA).
	// Check under the writer bit to avoid a rare duplicate cache slot.
	matches := stringCacheMatches(control, stringCacheControlByte(offset))
	for matches != 0 {
		slot := uint(bits.TrailingZeros64(matches)) / 8
		if cached := bucket.entries[slot].Load(); cached != nil && cached.offset == offset {
			recent.CompareAndSwap(recorded, remaining)
			referenced := uint64(1) << (stringCacheReferencedShift + slot)
			bucket.releaseWriter(control | referenced)
			return cached.str
		}
		matches &= matches - 1
	}
	slot, ok := stringCacheFreeSlot(control, home)

	// A full, fully referenced bucket is aged first. The next admission may
	// replace an entry that has remained unused since then.
	if !ok {
		// Lookups must begin tracking hits before reference bits are aged.
		if !table.searching.Load() {
			table.searching.Store(true)
		}
		bucket.control.Store(control &^ stringCacheReferencedBits)
		return string(value)
	}

	if !recent.CompareAndSwap(recorded, remaining) {
		bucket.releaseWriter(control)
		return string(value)
	}
	// Enable bucket scans only for an admitted off-home entry, before publishing it.
	if slot != home && !table.searching.Load() {
		table.searching.Store(true)
	}

	// Publish the entry before its control byte. Racing readers may miss, but
	// offset checks prevent a false hit.
	referenced := uint64(1) << (stringCacheReferencedShift + slot)
	updated := control&^(0xFF<<(8*slot)) | stringCacheControlByte(offset)<<(8*slot) | referenced
	str := string(value)
	bucket.entries[slot].Store(&cacheEntry{
		str:    str,
		offset: offset,
	})
	bucket.releaseWriter(updated)
	recordStringCacheAdmission()

	return str
}

func (table *stringCacheTable) recentMiss(offset uint) *atomic.Uint64 {
	return &table.recentMisses[(offset>>stringCacheMissShift)%stringCacheMisses]
}

// releaseWriter publishes control metadata and releases the publisher lock.
func (bucket *cacheBucket) releaseWriter(updated uint64) {
	// Readers can reference entries while the publisher lock is held.
	// Preserve those hits when publishing or abandoning an admission.
	for {
		current := bucket.control.Load()
		merged := updated | (current & stringCacheReferencedBits)
		if bucket.control.CompareAndSwap(current, merged) {
			return
		}
	}
}

// stringCacheAdmissionValue biases the truncated offset away from zero.
// The miss path bypasses caching when the token wraps to the empty sentinel.
func stringCacheAdmissionValue(offset uint) uint32 {
	return uint32(offset) + 1
}

func stringCacheTakeMiss(recorded uint64, admissionValue uint32) (uint64, bool) {
	switch admissionValue {
	case uint32(recorded):
		return recorded >> 32, true
	case uint32(recorded >> 32):
		return recorded & math.MaxUint32, true
	}
	return recorded, false
}

// stringCacheHomeSlot maps strings at least three bytes apart to distinct
// slots within a window.
func stringCacheHomeSlot(offset uint) uint {
	return (offset & stringCacheWindowMask) * stringCacheBucketSlots >> stringCacheWindowBits
}

// stringCacheControlByte combines the window position and table pass into a
// candidate filter; the entry's full offset decides a hit.
func stringCacheControlByte(offset uint) uint64 {
	const passBits = 0x7F >> stringCacheWindowBits

	position := offset & stringCacheWindowMask
	pass := (offset >> stringCachePassShift) & passBits
	return stringCacheOccupied | uint64(pass<<stringCacheWindowBits|position)
}

// stringCacheMatches filters candidate slots. Borrows may create false matches,
// so callers must verify each entry's offset.
func stringCacheMatches(control, want uint64) uint64 {
	differences := control ^ (want * stringCacheSlotLowBits)
	return (differences - stringCacheSlotLowBits) &^ differences & stringCacheSlotHighBits
}

func stringCacheFreeSlot(control uint64, home uint) (uint, bool) {
	if control&(stringCacheOccupied<<(8*home)) == 0 {
		return home, true
	}
	if empty := ^control & stringCacheSlotHighBits; empty != 0 {
		return uint(bits.TrailingZeros64(empty)) / 8, true
	}

	unused := uint(^control>>stringCacheReferencedShift) & stringCacheAllSlots
	if unused == 0 {
		return 0, false
	}
	rotated := (unused>>home | unused<<(stringCacheBucketSlots-home)) & stringCacheAllSlots
	return (home + uint(bits.TrailingZeros(rotated))) % stringCacheBucketSlots, true
}
