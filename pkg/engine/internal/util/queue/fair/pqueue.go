package fair

// posNode is a node in a pqueue, stored with its position.
type posNode[T any] struct {
	*node[T]

	// Index in the pqueue. -1 if the node is registered as a
	// scope but is not in the pqueue (because it is empty).
	Index int
}

// pqueue is the priority queue for [nodeKindIntermediate] nodes.
type pqueue[T any] struct {
	scope Scope // Scope of this pqueue.

	scopeLookup map[string]*posNode[T] // Lookup for children scopes
	children    []*posNode[T]
}

// Len returns the number of children in pq.
func (pq *pqueue[T]) Len() int { return len(pq.children) }

// Less returns true if the node at index i has a lower rank than the node at
// index j.
func (pq *pqueue[T]) Less(i, j int) bool {
	if pq.children[i].rank == pq.children[j].rank {
		return pq.children[i].id < pq.children[j].id
	}
	return pq.children[i].rank < pq.children[j].rank
}

// Swap swaps the elements at index i and j.
func (pq *pqueue[T]) Swap(i, j int) {
	pq.children[i], pq.children[j] = pq.children[j], pq.children[i]
	pq.children[i].Index, pq.children[j].Index = i, j
}

// Push adds a new element to the end of pq.
func (pq *pqueue[T]) Push(x any) {
	pn := &posNode[T]{node: x.(*node[T]), Index: len(pq.children)}
	pq.children = append(pq.children, pn)
}

// Pop removes and returns the element at pq.Len()-1.
//
// If the element is a scope node, it is *not* unregistered.
func (pq *pqueue[T]) Pop() any {
	rem := pq.children[len(pq.children)-1]
	pq.children = pq.children[:len(pq.children)-1]
	rem.Index = -1
	return rem
}
