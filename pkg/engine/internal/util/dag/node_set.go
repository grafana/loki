package dag

// nodeSet is a set of nodes in an arbitrary order.
type nodeSet[N Node] map[N]struct{}

// Add adds the given node to the set. If node is the zero value, Add is a
// no-op.
func (s nodeSet[N]) Add(node N) {
	if isZero(node) {
		return
	}
	s[node] = struct{}{}
}

// Contains returns true if the given node is in the set.
func (s nodeSet[N]) Contains(node N) bool {
	_, ok := s[node]
	return ok
}
