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
	table atomic.Pointer[typeTable[T]]
	// mu is for the writers. A value is stored only when a type is compiled.
	mu sync.Mutex
}

type typeTable[T any] struct {
	entries []typeEntry[T]
	// shift makes an index of entries from the hash.
	shift uint
	// count is the number of the entries in use. It is guarded by TypeCache.mu.
	count int
}

// typeEntry is written once: the value, and then the type, which makes it visible.
type typeEntry[T any] struct {
	typ   atomic.Uintptr
	value atomic.Pointer[T]
}

const (
	minTypeTableBits = 6
)

// TypeHashMultiplier is the multiplier of the Fibonacci hashing of the address of a type: the top bits of the
// product spread the addresses, which are aligned and close to each other, over a table.
const TypeHashMultiplier = 0x9E3779B97F4A7C15

func newTypeTable[T any](bits uint) *typeTable[T] {
	return &typeTable[T]{
		entries: make([]typeEntry[T], 1<<bits),
		shift:   64 - bits,
	}
}

func (t *typeTable[T]) index(typ uintptr) uintptr {
	return uintptr((uint64(typ) * TypeHashMultiplier) >> t.shift)
}

// entry returns the entry of the index, which is always less than the length:
// the check of the bounds is not worth its cost on this path, which every Marshal and Unmarshal takes.
func (t *typeTable[T]) entry(index uintptr) *typeEntry[T] {
	return (*typeEntry[T])(unsafe.Add(unsafe.Pointer(unsafe.SliceData(t.entries)), index*unsafe.Sizeof(typeEntry[T]{})))
}

// Load returns the value for the type, or nil.
func (c *TypeCache[T]) Load(typ uintptr) *T {
	t := c.table.Load()
	if t == nil {
		return nil
	}
	mask := uintptr(len(t.entries) - 1)
	for i := t.index(typ); ; i = (i + 1) & mask {
		e := t.entry(i)
		switch e.typ.Load() {
		case typ:
			return e.value.Load()
		case 0:
			return nil
		}
	}
}

// Store sets the value for the type, and returns the value which the table has for the type:
// it is the value which was stored first if the type was compiled by more than one goroutine at a time.
func (c *TypeCache[T]) Store(typ uintptr, v *T) *T {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := c.table.Load()
	if t == nil {
		t = newTypeTable[T](minTypeTableBits)
		c.table.Store(t)
	}
	if existing := c.Load(typ); existing != nil {
		return existing
	}
	// the table is at most half full, so a lookup ends after a few entries.
	if (t.count+1)*2 > len(t.entries) {
		grown := newTypeTable[T](64 - t.shift + 1)
		for i := range t.entries {
			e := &t.entries[i]
			if typ := e.typ.Load(); typ != 0 {
				grown.insert(typ, e.value.Load())
			}
		}
		grown.insert(typ, v)
		// the table gets visible after it has every value.
		c.table.Store(grown)
		return v
	}
	t.insert(typ, v)
	return v
}

func (t *typeTable[T]) insert(typ uintptr, v *T) {
	mask := uintptr(len(t.entries) - 1)
	for i := t.index(typ); ; i = (i + 1) & mask {
		e := t.entry(i)
		if e.typ.Load() == 0 {
			// the value is set before the entry gets visible by the type.
			e.value.Store(v)
			e.typ.Store(typ)
			t.count++
			return
		}
	}
}
