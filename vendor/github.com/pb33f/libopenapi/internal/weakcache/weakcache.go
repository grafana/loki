// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package weakcache provides a concurrency-safe cache keyed by pointer identity that does not keep its keys
// alive.
package weakcache

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"
)

// Cache maps pointers to values without retaining the pointers. Entries are found by address and verified
// through a weak pointer, so a hit costs a map lookup, an entry is dropped once the garbage collector reclaims
// its key, and an object later allocated at a reused address never sees the entry of the object that lived
// there before.
//
// The zero value is ready to use. Keys must not be nil.
type Cache[K, V any] struct {
	entries    sync.Map // uintptr -> *entry[K, V]
	generation atomic.Uint64
}

type entry[K, V any] struct {
	key   weak.Pointer[K]
	value atomic.Pointer[stamped[V]]
}

type stamped[V any] struct {
	generation uint64
	value      V
}

// Load returns the value stored for key, if there is one and it was stored after the last Clear.
func (c *Cache[K, V]) Load(key *K) (V, bool) {
	if held := c.held(key); held != nil {
		if s := held.value.Load(); s.generation == c.generation.Load() {
			return s.value, true
		}
	}
	var zero V
	return zero, false
}

// Store records value for key. The entry is dropped automatically after key becomes unreachable.
func (c *Cache[K, V]) Store(key *K, value V) {
	s := &stamped[V]{generation: c.generation.Load(), value: value}
	if held := c.held(key); held != nil {
		held.value.Store(s)
		return
	}
	// there is no entry for this address, or only one left behind by a reclaimed object that lived there.
	addr := uintptr(unsafe.Pointer(key))
	fresh := &entry[K, V]{key: weak.Make(key)}
	fresh.value.Store(s)
	c.entries.Store(addr, fresh)
	runtime.AddCleanup(key, func(stale *entry[K, V]) {
		c.entries.CompareAndDelete(addr, stale)
	}, fresh)
}

// Clear invalidates every entry. Invalidated entries for live keys are updated in place by their next Store and
// entries for unreachable keys are removed as the keys are reclaimed, so clearing never registers a second
// cleanup for a key that is still alive.
func (c *Cache[K, V]) Clear() {
	c.generation.Add(1)
}

// Len reports the number of entries currently held, including invalidated entries that have not been replaced
// or reclaimed yet.
func (c *Cache[K, V]) Len() int {
	n := 0
	c.entries.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// held returns the entry stored for key, or nil when the address has no entry or its entry belongs to an object
// that has been reclaimed.
func (c *Cache[K, V]) held(key *K) *entry[K, V] {
	if e, ok := c.entries.Load(uintptr(unsafe.Pointer(key))); ok {
		if held := e.(*entry[K, V]); held.key.Value() == key {
			return held
		}
	}
	return nil
}
