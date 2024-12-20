// Package intmap contains a fast hashmap implementation for maps with keys of any integer type
package intmap

import (
	"iter"
	"math"
)

// IntKey is a type constraint for values that can be used as keys in Map
type IntKey interface {
	~int | ~uint | ~int64 | ~uint64 | ~int32 | ~uint32 | ~int16 | ~uint16 | ~int8 | ~uint8 | ~uintptr
}

type pair[K IntKey, V any] struct {
	K K
	V V
}

const fillFactor64 = 0.7

func phiMix64(x int) int {
	h := int64(x) * int64(0x9E3779B9)
	return int(h ^ (h >> 16))
}

// Map is a hashmap where the keys are some any integer type.
// It is valid to call methods that read a nil map, similar to a standard Go map.
// Methods valid on a nil map are Has, Get, Len, and ForEach.
type Map[K IntKey, V any] struct {
	data []pair[K, V] // key-value pairs
	size int

	zeroVal    V    // value of 'zero' key
	hasZeroKey bool // do we have 'zero' key in the map?
}

// New creates a new map with keys being any integer subtype.
// The map can store up to the given capacity before reallocation and rehashing occurs.
func New[K IntKey, V any](capacity int) *Map[K, V] {
	return &Map[K, V]{
		data: make([]pair[K, V], arraySize(capacity, fillFactor64)),
	}
}

// Has checks if the given key exists in the map.
// Calling this method on a nil map will return false.
func (m *Map[K, V]) Has(key K) bool {
	if m == nil {
		return false
	}

	if key == K(0) {
		return m.hasZeroKey
	}

	idx := m.startIndex(key)
	p := m.data[idx]

	if p.K == K(0) { // end of chain already
		return false
	}
	if p.K == key { // we check zero prior to this call
		return true
	}

	// hash collision, seek next hash match, bailing on first empty
	for {
		idx = m.nextIndex(idx)
		p = m.data[idx]
		if p.K == K(0) {
			return false
		}
		if p.K == key {
			return true
		}
	}
}

// Get returns the value if the key is found.
// If you just need to check for existence it is easier to use Has.
// Calling this method on a nil map will return the zero value for V and false.
func (m *Map[K, V]) Get(key K) (V, bool) {
	if m == nil {
		var zero V
		return zero, false
	}

	if key == K(0) {
		if m.hasZeroKey {
			return m.zeroVal, true
		}
		var zero V
		return zero, false
	}

	idx := m.startIndex(key)
	p := m.data[idx]

	if p.K == K(0) { // end of chain already
		var zero V
		return zero, false
	}
	if p.K == key { // we check zero prior to this call
		return p.V, true
	}

	// hash collision, seek next hash match, bailing on first empty
	for {
		idx = m.nextIndex(idx)
		p = m.data[idx]
		if p.K == K(0) {
			var zero V
			return zero, false
		}
		if p.K == key {
			return p.V, true
		}
	}
}

// Put adds or updates key with value val.
func (m *Map[K, V]) Put(key K, val V) {
	if key == K(0) {
		if !m.hasZeroKey {
			m.size++
		}
		m.zeroVal = val
		m.hasZeroKey = true
		return
	}

	idx := m.startIndex(key)
	p := &m.data[idx]

	if p.K == K(0) { // end of chain already
		p.K = key
		p.V = val
		if m.size >= m.sizeThreshold() {
			m.rehash()
		} else {
			m.size++
		}
		return
	} else if p.K == key { // overwrite existing value
		p.V = val
		return
	}

	// hash collision, seek next empty or key match
	for {
		idx = m.nextIndex(idx)
		p = &m.data[idx]

		if p.K == K(0) {
			p.K = key
			p.V = val
			if m.size >= m.sizeThreshold() {
				m.rehash()
			} else {
				m.size++
			}
			return
		} else if p.K == key {
			p.V = val
			return
		}
	}
}

// PutIfNotExists adds the key-value pair only if the key does not already exist
// in the map, and returns the current value associated with the key and a boolean
// indicating whether the value was newly added or not.
func (m *Map[K, V]) PutIfNotExists(key K, val V) (V, bool) {
	if key == K(0) {
		if m.hasZeroKey {
			return m.zeroVal, false
		}
		m.zeroVal = val
		m.hasZeroKey = true
		m.size++
		return val, true
	}

	idx := m.startIndex(key)
	p := &m.data[idx]

	if p.K == K(0) { // end of chain already
		p.K = key
		p.V = val
		m.size++
		if m.size >= m.sizeThreshold() {
			m.rehash()
		}
		return val, true
	} else if p.K == key {
		return p.V, false
	}

	// hash collision, seek next hash match, bailing on first empty
	for {
		idx = m.nextIndex(idx)
		p = &m.data[idx]

		if p.K == K(0) {
			p.K = key
			p.V = val
			m.size++
			if m.size >= m.sizeThreshold() {
				m.rehash()
			}
			return val, true
		} else if p.K == key {
			return p.V, false
		}
	}
}

// ForEach iterates through key-value pairs in the map while the function f returns true.
// This method returns immediately if invoked on a nil map.
//
// The iteration order of a Map is not defined, so please avoid relying on it.
func (m *Map[K, V]) ForEach(f func(K, V) bool) {
	if m == nil {
		return
	}

	if m.hasZeroKey && !f(K(0), m.zeroVal) {
		return
	}
	forEach64(m.data, f)
}

// All returns an iterator over key-value pairs from m.
// The iterator returns immediately if invoked on a nil map.
//
// The iteration order of a Map is not defined, so please avoid relying on it.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return m.ForEach
}

// Keys returns an iterator over keys in m.
// The iterator returns immediately if invoked on a nil map.
//
// The iteration order of a Map is not defined, so please avoid relying on it.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(k K) bool) {
		if m == nil {
			return
		}

		if m.hasZeroKey && !yield(K(0)) {
			return
		}

		for _, p := range m.data {
			if p.K != K(0) && !yield(p.K) {
				return
			}
		}
	}
}

// Values returns an iterator over values in m.
// The iterator returns immediately if invoked on a nil map.
//
// The iteration order of a Map is not defined, so please avoid relying on it.
func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(v V) bool) {
		if m == nil {
			return
		}

		if m.hasZeroKey && !yield(m.zeroVal) {
			return
		}

		for _, p := range m.data {
			if p.K != K(0) && !yield(p.V) {
				return
			}
		}
	}
}

// Clear removes all items from the map, but keeps the internal buffers for reuse.
func (m *Map[K, V]) Clear() {
	var zero V
	m.hasZeroKey = false
	m.zeroVal = zero

	// compiles down to runtime.memclr()
	for i := range m.data {
		m.data[i] = pair[K, V]{}
	}

	m.size = 0
}

func (m *Map[K, V]) rehash() {
	oldData := m.data
	m.data = make([]pair[K, V], 2*len(m.data))

	// reset size
	if m.hasZeroKey {
		m.size = 1
	} else {
		m.size = 0
	}

	forEach64(oldData, func(k K, v V) bool {
		m.Put(k, v)
		return true
	})
}

// Len returns the number of elements in the map.
// The length of a nil map is defined to be zero.
func (m *Map[K, V]) Len() int {
	if m == nil {
		return 0
	}

	return m.size
}

func (m *Map[K, V]) sizeThreshold() int {
	return int(math.Floor(float64(len(m.data)) * fillFactor64))
}

func (m *Map[K, V]) startIndex(key K) int {
	return phiMix64(int(key)) & (len(m.data) - 1)
}

func (m *Map[K, V]) nextIndex(idx int) int {
	return (idx + 1) & (len(m.data) - 1)
}

func forEach64[K IntKey, V any](pairs []pair[K, V], f func(k K, v V) bool) {
	for _, p := range pairs {
		if p.K != K(0) && !f(p.K, p.V) {
			return
		}
	}
}

// Del deletes a key and its value, returning true iff the key was found
func (m *Map[K, V]) Del(key K) bool {
	if key == K(0) {
		if m.hasZeroKey {
			m.hasZeroKey = false
			m.size--
			return true
		}
		return false
	}

	idx := m.startIndex(key)
	p := m.data[idx]

	if p.K == key {
		// any keys that were pushed back needs to be shifted nack into the empty slot
		// to avoid breaking the chain
		m.shiftKeys(idx)
		m.size--
		return true
	} else if p.K == K(0) { // end of chain already
		return false
	}

	for {
		idx = m.nextIndex(idx)
		p = m.data[idx]

		if p.K == key {
			// any keys that were pushed back needs to be shifted nack into the empty slot
			// to avoid breaking the chain
			m.shiftKeys(idx)
			m.size--
			return true
		} else if p.K == K(0) {
			return false
		}

	}
}

func (m *Map[K, V]) shiftKeys(idx int) int {
	// Shift entries with the same hash.
	// We need to do this on deletion to ensure we don't have zeroes in the hash chain
	for {
		var p pair[K, V]
		lastIdx := idx
		idx = m.nextIndex(idx)
		for {
			p = m.data[idx]
			if p.K == K(0) {
				m.data[lastIdx] = pair[K, V]{}
				return lastIdx
			}

			slot := m.startIndex(p.K)
			if lastIdx <= idx {
				if lastIdx >= slot || slot > idx {
					break
				}
			} else {
				if lastIdx >= slot && slot > idx {
					break
				}
			}
			idx = m.nextIndex(idx)
		}
		m.data[lastIdx] = p
	}
}

func nextPowerOf2(x uint32) uint32 {
	if x == math.MaxUint32 {
		return x
	}

	if x == 0 {
		return 1
	}

	x--
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16

	return x + 1
}

func arraySize(exp int, fill float64) int {
	s := nextPowerOf2(uint32(math.Ceil(float64(exp) / fill)))
	if s < 2 {
		s = 2
	}
	return int(s)
}
