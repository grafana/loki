// Package symbolizer provides a string interning mechanism to reduce memory usage
// by reusing identical strings.
//
// The Symbolizer maintains a cache of strings and returns the same instance
// when the same string is requested multiple times. This reduces memory usage
// when dealing with repeated strings, such as label names or values. It is not
// thread safe.
//
// When the cache exceeds the maximum size, a small percentage of entries are
// randomly discarded to keep memory usage under control.
package symbolizer

import (
	"strings"
)

// New creates a new Symbolizer with the given initial capacity and maximum size.
func New(initialCapacity int, maxSize int) *Symbolizer {
	return &Symbolizer{
		symbols: make(map[string]string, initialCapacity),
		maxSize: maxSize,
	}
}

type Symbolizer struct {
	symbols map[string]string
	maxSize int
}

// Get returns a string from the symbolizer. If the string is not in the cache,
// a clone is inserted into the cache and returned.
//
// Get may delete some values from the cache prior to inserting a new value if
// the maximum size is exceeded.
func (s *Symbolizer) Get(name string) string {
	if value, ok := s.symbols[name]; ok {
		return value
	}
	// Control maximum memory use by discarding a random 1% of symbols if the map gets too big.
	// We rely on Golang's unspecified map ordering to choose what to discard.
	if len(s.symbols) > s.maxSize {
		i := 0
		for k := range s.symbols {
			if i > s.maxSize/100 {
				break
			}
			delete(s.symbols, k)
			i++
		}
	}
	newString := strings.Clone(name)
	s.symbols[newString] = newString
	return newString
}

// Reset clears the cache and resets the Symbolizer to its initial state,
// maintaining the existing maxSize.
func (s *Symbolizer) Reset() {
	clear(s.symbols)
}
