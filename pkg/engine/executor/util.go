package executor

// newSet creates a new set of elements T with capacity n.
func newSet[T comparable](n int) set[T] {
	return make(set[T], n)
}

type set[T comparable] map[T]struct{}

// add adds element e to the set s.
// Returns true if the element was added, and false if the element already existed.
func (s set[T]) add(e T) bool {
	if _, ok := s[e]; ok {
		return false
	}
	s[e] = struct{}{}
	return true
}

// del deletes element e from the set s.
// Returns true if the element was deleted, and false if not.
func (s set[T]) del(e T) bool {
	if _, ok := s[e]; !ok {
		return false
	}
	delete(s, e)
	return true
}

// clean deletes all elements from the set s.
func (s set[T]) clean() {
	clear(s)
}
