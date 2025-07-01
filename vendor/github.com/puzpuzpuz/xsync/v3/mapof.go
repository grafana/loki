package xsync

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// number of MapOf entries per bucket; 5 entries lead to size of 64B
	// (one cache line) on 64-bit machines
	entriesPerMapOfBucket        = 5
	defaultMeta           uint64 = 0x8080808080808080
	metaMask              uint64 = 0xffffffffff
	defaultMetaMasked     uint64 = defaultMeta & metaMask
	emptyMetaSlot         uint8  = 0x80
)

// MapOf is like a Go map[K]V but is safe for concurrent
// use by multiple goroutines without additional locking or
// coordination. It follows the interface of sync.Map with
// a number of valuable extensions like Compute or Size.
//
// A MapOf must not be copied after first use.
//
// MapOf uses a modified version of Cache-Line Hash Table (CLHT)
// data structure: https://github.com/LPD-EPFL/CLHT
//
// CLHT is built around idea to organize the hash table in
// cache-line-sized buckets, so that on all modern CPUs update
// operations complete with at most one cache-line transfer.
// Also, Get operations involve no write to memory, as well as no
// mutexes or any other sort of locks. Due to this design, in all
// considered scenarios MapOf outperforms sync.Map.
//
// MapOf also borrows ideas from Java's j.u.c.ConcurrentHashMap
// (immutable K/V pair structs instead of atomic snapshots)
// and C++'s absl::flat_hash_map (meta memory and SWAR-based
// lookups).
type MapOf[K comparable, V any] struct {
	totalGrowths int64
	totalShrinks int64
	resizing     int64          // resize in progress flag; updated atomically
	resizeMu     sync.Mutex     // only used along with resizeCond
	resizeCond   sync.Cond      // used to wake up resize waiters (concurrent modifications)
	table        unsafe.Pointer // *mapOfTable
	hasher       func(K, uint64) uint64
	minTableLen  int
	growOnly     bool
}

type mapOfTable[K comparable, V any] struct {
	buckets []bucketOfPadded
	// striped counter for number of table entries;
	// used to determine if a table shrinking is needed
	// occupies min(buckets_memory/1024, 64KB) of memory
	size []counterStripe
	seed uint64
}

// bucketOfPadded is a CL-sized map bucket holding up to
// entriesPerMapOfBucket entries.
type bucketOfPadded struct {
	//lint:ignore U1000 ensure each bucket takes two cache lines on both 32 and 64-bit archs
	pad [cacheLineSize - unsafe.Sizeof(bucketOf{})]byte
	bucketOf
}

type bucketOf struct {
	meta    uint64
	entries [entriesPerMapOfBucket]unsafe.Pointer // *entryOf
	next    unsafe.Pointer                        // *bucketOfPadded
	mu      sync.Mutex
}

// entryOf is an immutable map entry.
type entryOf[K comparable, V any] struct {
	key   K
	value V
}

// NewMapOf creates a new MapOf instance configured with the given
// options.
func NewMapOf[K comparable, V any](options ...func(*MapConfig)) *MapOf[K, V] {
	return NewMapOfWithHasher[K, V](defaultHasher[K](), options...)
}

// NewMapOfWithHasher creates a new MapOf instance configured with
// the given hasher and options. The hash function is used instead
// of the built-in hash function configured when a map is created
// with the NewMapOf function.
func NewMapOfWithHasher[K comparable, V any](
	hasher func(K, uint64) uint64,
	options ...func(*MapConfig),
) *MapOf[K, V] {
	c := &MapConfig{
		sizeHint: defaultMinMapTableLen * entriesPerMapOfBucket,
	}
	for _, o := range options {
		o(c)
	}

	m := &MapOf[K, V]{}
	m.resizeCond = *sync.NewCond(&m.resizeMu)
	m.hasher = hasher
	var table *mapOfTable[K, V]
	if c.sizeHint <= defaultMinMapTableLen*entriesPerMapOfBucket {
		table = newMapOfTable[K, V](defaultMinMapTableLen)
	} else {
		tableLen := nextPowOf2(uint32((float64(c.sizeHint) / entriesPerMapOfBucket) / mapLoadFactor))
		table = newMapOfTable[K, V](int(tableLen))
	}
	m.minTableLen = len(table.buckets)
	m.growOnly = c.growOnly
	atomic.StorePointer(&m.table, unsafe.Pointer(table))
	return m
}

// NewMapOfPresized creates a new MapOf instance with capacity enough
// to hold sizeHint entries. The capacity is treated as the minimal capacity
// meaning that the underlying hash table will never shrink to
// a smaller capacity. If sizeHint is zero or negative, the value
// is ignored.
//
// Deprecated: use NewMapOf in combination with WithPresize.
func NewMapOfPresized[K comparable, V any](sizeHint int) *MapOf[K, V] {
	return NewMapOf[K, V](WithPresize(sizeHint))
}

func newMapOfTable[K comparable, V any](minTableLen int) *mapOfTable[K, V] {
	buckets := make([]bucketOfPadded, minTableLen)
	for i := range buckets {
		buckets[i].meta = defaultMeta
	}
	counterLen := minTableLen >> 10
	if counterLen < minMapCounterLen {
		counterLen = minMapCounterLen
	} else if counterLen > maxMapCounterLen {
		counterLen = maxMapCounterLen
	}
	counter := make([]counterStripe, counterLen)
	t := &mapOfTable[K, V]{
		buckets: buckets,
		size:    counter,
		seed:    makeSeed(),
	}
	return t
}

// ToPlainMapOf returns a native map with a copy of xsync Map's
// contents. The copied xsync Map should not be modified while
// this call is made. If the copied Map is modified, the copying
// behavior is the same as in the Range method.
func ToPlainMapOf[K comparable, V any](m *MapOf[K, V]) map[K]V {
	pm := make(map[K]V)
	if m != nil {
		m.Range(func(key K, value V) bool {
			pm[key] = value
			return true
		})
	}
	return pm
}

// Load returns the value stored in the map for a key, or zero value
// of type V if no value is present.
// The ok result indicates whether value was found in the map.
func (m *MapOf[K, V]) Load(key K) (value V, ok bool) {
	table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
	hash := m.hasher(key, table.seed)
	h1 := h1(hash)
	h2w := broadcast(h2(hash))
	bidx := uint64(len(table.buckets)-1) & h1
	b := &table.buckets[bidx]
	for {
		metaw := atomic.LoadUint64(&b.meta)
		markedw := markZeroBytes(metaw^h2w) & metaMask
		for markedw != 0 {
			idx := firstMarkedByteIndex(markedw)
			eptr := atomic.LoadPointer(&b.entries[idx])
			if eptr != nil {
				e := (*entryOf[K, V])(eptr)
				if e.key == key {
					return e.value, true
				}
			}
			markedw &= markedw - 1
		}
		bptr := atomic.LoadPointer(&b.next)
		if bptr == nil {
			return
		}
		b = (*bucketOfPadded)(bptr)
	}
}

// Store sets the value for a key.
func (m *MapOf[K, V]) Store(key K, value V) {
	m.doCompute(
		key,
		func(V, bool) (V, bool) {
			return value, false
		},
		false,
		false,
	)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *MapOf[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	return m.doCompute(
		key,
		func(V, bool) (V, bool) {
			return value, false
		},
		true,
		false,
	)
}

// LoadAndStore returns the existing value for the key if present,
// while setting the new value for the key.
// It stores the new value and returns the existing one, if present.
// The loaded result is true if the existing value was loaded,
// false otherwise.
func (m *MapOf[K, V]) LoadAndStore(key K, value V) (actual V, loaded bool) {
	return m.doCompute(
		key,
		func(V, bool) (V, bool) {
			return value, false
		},
		false,
		false,
	)
}

// LoadOrCompute returns the existing value for the key if present.
// Otherwise, it computes the value using the provided function, and
// then stores and returns the computed value. The loaded result is
// true if the value was loaded, false if computed.
//
// This call locks a hash table bucket while the compute function
// is executed. It means that modifications on other entries in
// the bucket will be blocked until the valueFn executes. Consider
// this when the function includes long-running operations.
func (m *MapOf[K, V]) LoadOrCompute(key K, valueFn func() V) (actual V, loaded bool) {
	return m.doCompute(
		key,
		func(V, bool) (V, bool) {
			return valueFn(), false
		},
		true,
		false,
	)
}

// LoadOrTryCompute returns the existing value for the key if present.
// Otherwise, it tries to compute the value using the provided function
// and, if successful, stores and returns the computed value. The loaded
// result is true if the value was loaded, or false if computed (whether
// successfully or not). If the compute attempt was cancelled (due to an
// error, for example), a zero value of type V will be returned.
//
// This call locks a hash table bucket while the compute function
// is executed. It means that modifications on other entries in
// the bucket will be blocked until the valueFn executes. Consider
// this when the function includes long-running operations.
func (m *MapOf[K, V]) LoadOrTryCompute(
	key K,
	valueFn func() (newValue V, cancel bool),
) (value V, loaded bool) {
	return m.doCompute(
		key,
		func(V, bool) (V, bool) {
			nv, c := valueFn()
			if !c {
				return nv, false
			}
			return nv, true // nv is ignored
		},
		true,
		false,
	)
}

// Compute either sets the computed new value for the key or deletes
// the value for the key. When the delete result of the valueFn function
// is set to true, the value will be deleted, if it exists. When delete
// is set to false, the value is updated to the newValue.
// The ok result indicates whether value was computed and stored, thus, is
// present in the map. The actual result contains the new value in cases where
// the value was computed and stored. See the example for a few use cases.
//
// This call locks a hash table bucket while the compute function
// is executed. It means that modifications on other entries in
// the bucket will be blocked until the valueFn executes. Consider
// this when the function includes long-running operations.
func (m *MapOf[K, V]) Compute(
	key K,
	valueFn func(oldValue V, loaded bool) (newValue V, delete bool),
) (actual V, ok bool) {
	return m.doCompute(key, valueFn, false, true)
}

// LoadAndDelete deletes the value for a key, returning the previous
// value if any. The loaded result reports whether the key was
// present.
func (m *MapOf[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	return m.doCompute(
		key,
		func(value V, loaded bool) (V, bool) {
			return value, true
		},
		false,
		false,
	)
}

// Delete deletes the value for a key.
func (m *MapOf[K, V]) Delete(key K) {
	m.doCompute(
		key,
		func(value V, loaded bool) (V, bool) {
			return value, true
		},
		false,
		false,
	)
}

func (m *MapOf[K, V]) doCompute(
	key K,
	valueFn func(oldValue V, loaded bool) (V, bool),
	loadIfExists, computeOnly bool,
) (V, bool) {
	// Read-only path.
	if loadIfExists {
		if v, ok := m.Load(key); ok {
			return v, !computeOnly
		}
	}
	// Write path.
	for {
	compute_attempt:
		var (
			emptyb   *bucketOfPadded
			emptyidx int
		)
		table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
		tableLen := len(table.buckets)
		hash := m.hasher(key, table.seed)
		h1 := h1(hash)
		h2 := h2(hash)
		h2w := broadcast(h2)
		bidx := uint64(len(table.buckets)-1) & h1
		rootb := &table.buckets[bidx]
		rootb.mu.Lock()
		// The following two checks must go in reverse to what's
		// in the resize method.
		if m.resizeInProgress() {
			// Resize is in progress. Wait, then go for another attempt.
			rootb.mu.Unlock()
			m.waitForResize()
			goto compute_attempt
		}
		if m.newerTableExists(table) {
			// Someone resized the table. Go for another attempt.
			rootb.mu.Unlock()
			goto compute_attempt
		}
		b := rootb
		for {
			metaw := b.meta
			markedw := markZeroBytes(metaw^h2w) & metaMask
			for markedw != 0 {
				idx := firstMarkedByteIndex(markedw)
				eptr := b.entries[idx]
				if eptr != nil {
					e := (*entryOf[K, V])(eptr)
					if e.key == key {
						if loadIfExists {
							rootb.mu.Unlock()
							return e.value, !computeOnly
						}
						// In-place update/delete.
						// We get a copy of the value via an interface{} on each call,
						// thus the live value pointers are unique. Otherwise atomic
						// snapshot won't be correct in case of multiple Store calls
						// using the same value.
						oldv := e.value
						newv, del := valueFn(oldv, true)
						if del {
							// Deletion.
							// First we update the hash, then the entry.
							newmetaw := setByte(metaw, emptyMetaSlot, idx)
							atomic.StoreUint64(&b.meta, newmetaw)
							atomic.StorePointer(&b.entries[idx], nil)
							rootb.mu.Unlock()
							table.addSize(bidx, -1)
							// Might need to shrink the table if we left bucket empty.
							if newmetaw == defaultMeta {
								m.resize(table, mapShrinkHint)
							}
							return oldv, !computeOnly
						}
						newe := new(entryOf[K, V])
						newe.key = key
						newe.value = newv
						atomic.StorePointer(&b.entries[idx], unsafe.Pointer(newe))
						rootb.mu.Unlock()
						if computeOnly {
							// Compute expects the new value to be returned.
							return newv, true
						}
						// LoadAndStore expects the old value to be returned.
						return oldv, true
					}
				}
				markedw &= markedw - 1
			}
			if emptyb == nil {
				// Search for empty entries (up to 5 per bucket).
				emptyw := metaw & defaultMetaMasked
				if emptyw != 0 {
					idx := firstMarkedByteIndex(emptyw)
					emptyb = b
					emptyidx = idx
				}
			}
			if b.next == nil {
				if emptyb != nil {
					// Insertion into an existing bucket.
					var zeroV V
					newValue, del := valueFn(zeroV, false)
					if del {
						rootb.mu.Unlock()
						return zeroV, false
					}
					newe := new(entryOf[K, V])
					newe.key = key
					newe.value = newValue
					// First we update meta, then the entry.
					atomic.StoreUint64(&emptyb.meta, setByte(emptyb.meta, h2, emptyidx))
					atomic.StorePointer(&emptyb.entries[emptyidx], unsafe.Pointer(newe))
					rootb.mu.Unlock()
					table.addSize(bidx, 1)
					return newValue, computeOnly
				}
				growThreshold := float64(tableLen) * entriesPerMapOfBucket * mapLoadFactor
				if table.sumSize() > int64(growThreshold) {
					// Need to grow the table. Then go for another attempt.
					rootb.mu.Unlock()
					m.resize(table, mapGrowHint)
					goto compute_attempt
				}
				// Insertion into a new bucket.
				var zeroV V
				newValue, del := valueFn(zeroV, false)
				if del {
					rootb.mu.Unlock()
					return newValue, false
				}
				// Create and append a bucket.
				newb := new(bucketOfPadded)
				newb.meta = setByte(defaultMeta, h2, 0)
				newe := new(entryOf[K, V])
				newe.key = key
				newe.value = newValue
				newb.entries[0] = unsafe.Pointer(newe)
				atomic.StorePointer(&b.next, unsafe.Pointer(newb))
				rootb.mu.Unlock()
				table.addSize(bidx, 1)
				return newValue, computeOnly
			}
			b = (*bucketOfPadded)(b.next)
		}
	}
}

func (m *MapOf[K, V]) newerTableExists(table *mapOfTable[K, V]) bool {
	curTablePtr := atomic.LoadPointer(&m.table)
	return uintptr(curTablePtr) != uintptr(unsafe.Pointer(table))
}

func (m *MapOf[K, V]) resizeInProgress() bool {
	return atomic.LoadInt64(&m.resizing) == 1
}

func (m *MapOf[K, V]) waitForResize() {
	m.resizeMu.Lock()
	for m.resizeInProgress() {
		m.resizeCond.Wait()
	}
	m.resizeMu.Unlock()
}

func (m *MapOf[K, V]) resize(knownTable *mapOfTable[K, V], hint mapResizeHint) {
	knownTableLen := len(knownTable.buckets)
	// Fast path for shrink attempts.
	if hint == mapShrinkHint {
		if m.growOnly ||
			m.minTableLen == knownTableLen ||
			knownTable.sumSize() > int64((knownTableLen*entriesPerMapOfBucket)/mapShrinkFraction) {
			return
		}
	}
	// Slow path.
	if !atomic.CompareAndSwapInt64(&m.resizing, 0, 1) {
		// Someone else started resize. Wait for it to finish.
		m.waitForResize()
		return
	}
	var newTable *mapOfTable[K, V]
	table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
	tableLen := len(table.buckets)
	switch hint {
	case mapGrowHint:
		// Grow the table with factor of 2.
		atomic.AddInt64(&m.totalGrowths, 1)
		newTable = newMapOfTable[K, V](tableLen << 1)
	case mapShrinkHint:
		shrinkThreshold := int64((tableLen * entriesPerMapOfBucket) / mapShrinkFraction)
		if tableLen > m.minTableLen && table.sumSize() <= shrinkThreshold {
			// Shrink the table with factor of 2.
			atomic.AddInt64(&m.totalShrinks, 1)
			newTable = newMapOfTable[K, V](tableLen >> 1)
		} else {
			// No need to shrink. Wake up all waiters and give up.
			m.resizeMu.Lock()
			atomic.StoreInt64(&m.resizing, 0)
			m.resizeCond.Broadcast()
			m.resizeMu.Unlock()
			return
		}
	case mapClearHint:
		newTable = newMapOfTable[K, V](m.minTableLen)
	default:
		panic(fmt.Sprintf("unexpected resize hint: %d", hint))
	}
	// Copy the data only if we're not clearing the map.
	if hint != mapClearHint {
		for i := 0; i < tableLen; i++ {
			copied := copyBucketOf(&table.buckets[i], newTable, m.hasher)
			newTable.addSizePlain(uint64(i), copied)
		}
	}
	// Publish the new table and wake up all waiters.
	atomic.StorePointer(&m.table, unsafe.Pointer(newTable))
	m.resizeMu.Lock()
	atomic.StoreInt64(&m.resizing, 0)
	m.resizeCond.Broadcast()
	m.resizeMu.Unlock()
}

func copyBucketOf[K comparable, V any](
	b *bucketOfPadded,
	destTable *mapOfTable[K, V],
	hasher func(K, uint64) uint64,
) (copied int) {
	rootb := b
	rootb.mu.Lock()
	for {
		for i := 0; i < entriesPerMapOfBucket; i++ {
			if b.entries[i] != nil {
				e := (*entryOf[K, V])(b.entries[i])
				hash := hasher(e.key, destTable.seed)
				bidx := uint64(len(destTable.buckets)-1) & h1(hash)
				destb := &destTable.buckets[bidx]
				appendToBucketOf(h2(hash), b.entries[i], destb)
				copied++
			}
		}
		if b.next == nil {
			rootb.mu.Unlock()
			return
		}
		b = (*bucketOfPadded)(b.next)
	}
}

// Range calls f sequentially for each key and value present in the
// map. If f returns false, range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot
// of the Map's contents: no key will be visited more than once, but
// if the value for any key is stored or deleted concurrently, Range
// may reflect any mapping for that key from any point during the
// Range call.
//
// It is safe to modify the map while iterating it, including entry
// creation, modification and deletion. However, the concurrent
// modification rule apply, i.e. the changes may be not reflected
// in the subsequently iterated entries.
func (m *MapOf[K, V]) Range(f func(key K, value V) bool) {
	var zeroPtr unsafe.Pointer
	// Pre-allocate array big enough to fit entries for most hash tables.
	bentries := make([]unsafe.Pointer, 0, 16*entriesPerMapOfBucket)
	tablep := atomic.LoadPointer(&m.table)
	table := *(*mapOfTable[K, V])(tablep)
	for i := range table.buckets {
		rootb := &table.buckets[i]
		b := rootb
		// Prevent concurrent modifications and copy all entries into
		// the intermediate slice.
		rootb.mu.Lock()
		for {
			for i := 0; i < entriesPerMapOfBucket; i++ {
				if b.entries[i] != nil {
					bentries = append(bentries, b.entries[i])
				}
			}
			if b.next == nil {
				rootb.mu.Unlock()
				break
			}
			b = (*bucketOfPadded)(b.next)
		}
		// Call the function for all copied entries.
		for j := range bentries {
			entry := (*entryOf[K, V])(bentries[j])
			if !f(entry.key, entry.value) {
				return
			}
			// Remove the reference to avoid preventing the copied
			// entries from being GCed until this method finishes.
			bentries[j] = zeroPtr
		}
		bentries = bentries[:0]
	}
}

// Clear deletes all keys and values currently stored in the map.
func (m *MapOf[K, V]) Clear() {
	table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
	m.resize(table, mapClearHint)
}

// Size returns current size of the map.
func (m *MapOf[K, V]) Size() int {
	table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
	return int(table.sumSize())
}

func appendToBucketOf(h2 uint8, entryPtr unsafe.Pointer, b *bucketOfPadded) {
	for {
		for i := 0; i < entriesPerMapOfBucket; i++ {
			if b.entries[i] == nil {
				b.meta = setByte(b.meta, h2, i)
				b.entries[i] = entryPtr
				return
			}
		}
		if b.next == nil {
			newb := new(bucketOfPadded)
			newb.meta = setByte(defaultMeta, h2, 0)
			newb.entries[0] = entryPtr
			b.next = unsafe.Pointer(newb)
			return
		}
		b = (*bucketOfPadded)(b.next)
	}
}

func (table *mapOfTable[K, V]) addSize(bucketIdx uint64, delta int) {
	cidx := uint64(len(table.size)-1) & bucketIdx
	atomic.AddInt64(&table.size[cidx].c, int64(delta))
}

func (table *mapOfTable[K, V]) addSizePlain(bucketIdx uint64, delta int) {
	cidx := uint64(len(table.size)-1) & bucketIdx
	table.size[cidx].c += int64(delta)
}

func (table *mapOfTable[K, V]) sumSize() int64 {
	sum := int64(0)
	for i := range table.size {
		sum += atomic.LoadInt64(&table.size[i].c)
	}
	return sum
}

func h1(h uint64) uint64 {
	return h >> 7
}

func h2(h uint64) uint8 {
	return uint8(h & 0x7f)
}

// Stats returns statistics for the MapOf. Just like other map
// methods, this one is thread-safe. Yet it's an O(N) operation,
// so it should be used only for diagnostics or debugging purposes.
func (m *MapOf[K, V]) Stats() MapStats {
	stats := MapStats{
		TotalGrowths: atomic.LoadInt64(&m.totalGrowths),
		TotalShrinks: atomic.LoadInt64(&m.totalShrinks),
		MinEntries:   math.MaxInt32,
	}
	table := (*mapOfTable[K, V])(atomic.LoadPointer(&m.table))
	stats.RootBuckets = len(table.buckets)
	stats.Counter = int(table.sumSize())
	stats.CounterLen = len(table.size)
	for i := range table.buckets {
		nentries := 0
		b := &table.buckets[i]
		stats.TotalBuckets++
		for {
			nentriesLocal := 0
			stats.Capacity += entriesPerMapOfBucket
			for i := 0; i < entriesPerMapOfBucket; i++ {
				if atomic.LoadPointer(&b.entries[i]) != nil {
					stats.Size++
					nentriesLocal++
				}
			}
			nentries += nentriesLocal
			if nentriesLocal == 0 {
				stats.EmptyBuckets++
			}
			if b.next == nil {
				break
			}
			b = (*bucketOfPadded)(atomic.LoadPointer(&b.next))
			stats.TotalBuckets++
		}
		if nentries < stats.MinEntries {
			stats.MinEntries = nentries
		}
		if nentries > stats.MaxEntries {
			stats.MaxEntries = nentries
		}
	}
	return stats
}
