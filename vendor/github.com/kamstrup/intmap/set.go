package intmap

import "iter"

// Set is a specialization of Map modelling a set of integers.
// Like Map, methods that read from the set are valid on the nil Set.
// This include Has, Len, and ForEach.
type Set[K IntKey] Map[K, struct{}]

// NewSet creates a new Set with a given initial capacity.
func NewSet[K IntKey](capacity int) *Set[K] {
	return (*Set[K])(New[K, struct{}](capacity))
}

// Add an element to the set. Returns true if the element was not already present.
func (s *Set[K]) Add(k K) bool {
	_, found := (*Map[K, struct{}])(s).PutIfNotExists(k, struct{}{})
	return found
}

// Del deletes a key, returning true iff the key was found
func (s *Set[K]) Del(k K) bool {
	return (*Map[K, struct{}])(s).Del(k)
}

// Clear removes all items from the Set, but keeps the internal buffers for reuse.
func (s *Set[K]) Clear() {
	(*Map[K, struct{}])(s).Clear()
}

// Has returns true if the key is in the set.
// If the set is nil this method always return false.
func (s *Set[K]) Has(k K) bool {
	return (*Map[K, struct{}])(s).Has(k)
}

// Len returns the number of elements in the set.
// If the set is nil this method return 0.
func (s *Set[K]) Len() int {
	return (*Map[K, struct{}])(s).Len()
}

// ForEach iterates over the elements in the set while the visit function returns true.
// This method returns immediately if the set is nil.
//
// The iteration order of a Set is not defined, so please avoid relying on it.
func (s *Set[K]) ForEach(visit func(k K) bool) {
	(*Map[K, struct{}])(s).ForEach(func(k K, _ struct{}) bool {
		return visit(k)
	})
}

// All returns an iterator over keys from the set.
// The iterator returns immediately if the set is nil.
//
// The iteration order of a Set is not defined, so please avoid relying on it.
func (s *Set[K]) All() iter.Seq[K] {
	return s.ForEach
}
