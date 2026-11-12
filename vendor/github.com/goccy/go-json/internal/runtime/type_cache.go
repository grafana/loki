package runtime

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// TypeCache is a table of the values for the types, which is looked up by the address of a type
// without a lock.
//
// It is a hash table with open addressing. It used to be an array which had a slot for every address a type of
// the program can be at, which was found from the type links of the runtime: that needed a linkname, a scan of
// every type at the start, memory in proportion to the size of the program, and a fallback for the types which
// are not in the type links. A lookup of this table costs about the same, and it has none of them.
//
// The zero value is ready to use.
type TypeCache[T any] struct {
	cache typeCache
}

// Load returns the value for the type, or nil. It is small enough to be inlined into its callers, which a call of
// a generic function or of a function with a loop and a switch is not: the lookup is done by typeCache, for values
// as pointers.
func (c *TypeCache[T]) Load(typ uintptr) *T {
	return (*T)(c.cache.load(typ))
}

// LoadFirst returns the value for the type if it is in the entry the type is hashed to, or nil: then Load is to be
// called. It is smaller than Load, without a loop.
func (c *TypeCache[T]) LoadFirst(typ uintptr) *T {
	return (*T)(c.cache.loadFirst(typ))
}

// Store sets the value for the type, and returns the value which the table has for the type:
// it is the value which was stored first if the type was compiled by more than one goroutine at a time.
func (c *TypeCache[T]) Store(typ uintptr, v *T) *T {
	return (*T)(c.cache.store(typ, unsafe.Pointer(v)))
}

// typeCache is TypeCache for the values as pointers. Its words are read and written by the functions of
// sync/atomic, which cost less to inline than the methods of the atomic types.
type typeCache struct {
	table unsafe.Pointer // *typeTable, read and written atomically
	// mu is for the writers. A value is stored only when a type is compiled.
	mu sync.Mutex
}

type typeTable struct {
	entries []typeEntry
	// shift makes an index of entries from the hash.
	shift uint
	// count is the number of the entries in use. It is guarded by typeCache.mu.
	count int
}

// typeEntry is written once: the value, and then the type, which makes it visible.
type typeEntry struct {
	typ   uintptr        // read and written atomically
	value unsafe.Pointer // read and written atomically
}

const (
	minTypeTableBits = 6
)

// TypeHashMultiplier is the multiplier of the Fibonacci hashing of the address of a type: the top bits of the
// product spread the addresses, which are aligned and close to each other, over a table.
const TypeHashMultiplier = 0x9E3779B97F4A7C15

func newTypeTable(bits uint) *typeTable {
	return &typeTable{
		entries: make([]typeEntry, 1<<bits),
		shift:   64 - bits,
	}
}

func (t *typeTable) index(typ uintptr) uintptr {
	return uintptr((uint64(typ) * TypeHashMultiplier) >> t.shift)
}

// entry returns the entry of the index, which is always less than the length:
// the check of the bounds is not worth its cost on this path, which every Marshal and Unmarshal takes.
func (t *typeTable) entry(index uintptr) *typeEntry {
	return (*typeEntry)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(t.entries)), index*unsafe.Sizeof(typeEntry{})))
}

// loadFirst looks at the entry the type is hashed to only: it has no loop, so that it costs little in the code of
// its callers.
func (c *typeCache) loadFirst(typ uintptr) unsafe.Pointer {
	t := (*typeTable)(atomic.LoadPointer(&c.table))
	if t == nil {
		return nil
	}
	if e := t.entry(t.index(typ)); atomic.LoadUintptr(&e.typ) == typ {
		return atomic.LoadPointer(&e.value)
	}
	return nil
}

// load looks at the entries from the one the type is hashed to, up to the type or a free entry.
func (c *typeCache) load(typ uintptr) unsafe.Pointer {
	t := (*typeTable)(atomic.LoadPointer(&c.table))
	if t == nil {
		return nil
	}
	mask := uintptr(len(t.entries) - 1)
	for i := t.index(typ); ; i = (i + 1) & mask {
		e := t.entry(i)
		switch atomic.LoadUintptr(&e.typ) {
		case typ:
			return atomic.LoadPointer(&e.value)
		case 0:
			return nil
		}
	}
}

func (c *typeCache) store(typ uintptr, v unsafe.Pointer) unsafe.Pointer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := (*typeTable)(atomic.LoadPointer(&c.table))
	if t == nil {
		t = newTypeTable(minTypeTableBits)
		atomic.StorePointer(&c.table, unsafe.Pointer(t))
	}
	if existing := c.load(typ); existing != nil {
		return existing
	}
	// the table is at most half full, so a lookup ends after a few entries.
	if (t.count+1)*2 > len(t.entries) {
		grown := newTypeTable(64 - t.shift + 1)
		for i := range t.entries {
			e := &t.entries[i]
			if typ := atomic.LoadUintptr(&e.typ); typ != 0 {
				grown.insert(typ, atomic.LoadPointer(&e.value))
			}
		}
		grown.insert(typ, v)
		// the table gets visible after it has every value.
		atomic.StorePointer(&c.table, unsafe.Pointer(grown))
		return v
	}
	t.insert(typ, v)
	return v
}

func (t *typeTable) insert(typ uintptr, v unsafe.Pointer) {
	mask := uintptr(len(t.entries) - 1)
	for i := t.index(typ); ; i = (i + 1) & mask {
		e := t.entry(i)
		if atomic.LoadUintptr(&e.typ) == 0 {
			// the value is set before the entry gets visible by the type.
			atomic.StorePointer(&e.value, v)
			atomic.StoreUintptr(&e.typ, typ)
			t.count++
			return
		}
	}
}
