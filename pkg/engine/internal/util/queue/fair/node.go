package fair

import "container/heap"

// node represents an individual element in the hierarchical queue tree.
// Leaf nodes hold values of T, while internal nodes hold [pqueue].
type node[T any] struct {
	parent *node[T]  // Parent this node belongs to.
	queue  *Queue[T] // Queue this node belongs to.

	// Rank of this node within its parent. The rank determines [Queue.Pop]
	// order: lower rank means higher priority.
	rank int64

	// id of this node, assigned when it is created. id is used to tiebreak when
	// two nodes have the same rank (lower id wins).
	id int64

	// value is either a pqueue[T] for internal nodes, or T for leaf elements.
	value any
}

// FixChild fixes the priority of a child after its rank has been modified.
func (n *node[T]) FixChild(name string) {
	pq := n.pqueue()
	child, ok := pq.scopeLookup[name]
	if !ok {
		return
	}
	heap.Fix(pq, child.Index)
}

func (n *node[T]) pqueue() *pqueue[T] {
	pq, ok := n.value.(*pqueue[T])
	if !ok {
		panic("node is not a scope node")
	}
	return pq
}

// Peek returns the next selected child. Panics if n is not a scope node.
func (n *node[T]) Peek() (*node[T], Scope) {
	pq := n.pqueue()
	if len(pq.children) == 0 {
		return nil, nil
	}
	return pq.children[0].node, pq.scope
}

// Pop removes the next selected child from the heap. Panics if n is not a scope node.
func (n *node[T]) Pop() (*node[T], Scope) {
	pq := n.pqueue()
	if len(pq.children) == 0 {
		return nil, nil
	}

	child := pq.children[0].node
	_ = heap.Remove(pq, 0)
	return child, pq.scope
}

// Scope returns the scope for this node. Panics if n is not a scope node.
func (n *node[T]) Scope() Scope { return n.pqueue().scope }

// NumItems returns the number of non-heap children of n.
//
// NumItems panics if n is not a scope node.
func (n *node[T]) NumItems() int {
	pq, ok := n.value.(*pqueue[T])
	if !ok {
		panic("node is not a scope node")
	}

	var total int
	for _, c := range pq.children {
		_, isItem := c.value.(T)
		if isItem {
			total++
		}
	}
	return total
}

// NumChildren returns the number of children scopes of n (alive and dead).
//
// NumChildren panics if n is not a scope node.
func (n *node[T]) NumChildren() int {
	pq := n.pqueue()
	return len(pq.scopeLookup)
}

// Len returns the number of values of n.
//
// Children panics if n is not a scope node.
func (n *node[T]) Len() int { return n.pqueue().Len() }

// MarkAlive flags a child scope as alive, indicating that it has children and
// should be considered for selection in the heap. MarkAlive is a no-op if the
// scope is already alive.
//
// MarkAlive panics if n is not a scope node.
func (n *node[T]) MarkAlive(name string) {
	pq := n.pqueue()
	pn, ok := pq.scopeLookup[name]
	if !ok || pn.Index >= 0 /* already alive */ {
		return
	}

	// Reviving a scope shouldn't jump in line in the priority queue, so we
	// give it a new ID and ensure that its rank is no lower than the minimum
	// rank among its siblings.
	{
		var initRank int64
		if sibling, _ := n.Peek(); sibling != nil {
			initRank = sibling.rank
		}
		pn.rank = max(initRank, pn.rank)
		pn.id = n.queue.getNextID()
	}

	pn.Index = len(pq.children)
	pq.children = append(pq.children, pn)
	heap.Fix(pq, pn.Index)
}

// MarkDead flags the provided scope name as dead (without unregistering it),
// indicating that it has no children and should not be considered for selection
// in the heap. MarkDead is a no-op if the scope is already dead.
//
// MarkDead panics if n is not a scope node.
func (n *node[T]) MarkDead(name string) {
	pq := n.pqueue()
	pn, ok := pq.scopeLookup[name]
	if !ok || pn.Index < 0 /* already dead */ {
		return
	}

	heap.Remove(pq, pn.Index)
	pn.Index = -1 // Flag as dead
}

// GetScope returns the child scope with the given name. Panics if n is not a
// scope node.
func (n *node[T]) GetScope(name string) (*node[T], bool) {
	pq := n.pqueue()
	if n, ok := pq.scopeLookup[name]; ok {
		return n.node, true
	}
	return nil, false
}

// UnregisterScope removes the child scope with the given name.
//
// UnregisterScope panics if n is not a scope node.
func (n *node[T]) UnregisterScope(name string) {
	pq := n.pqueue()
	n.MarkDead(name)             // Remove it from the heap first,
	delete(pq.scopeLookup, name) // then delete the scope.
}

// RegisterScope creates a child scope. Returns an error if the scope already
// exists.
//
// RegisterScope panics if n is not a scope node. The scope is not validated to
// ensure that it is an immediate child of n.
func (n *node[T]) RegisterScope(scope Scope) (*node[T], error) {
	if len(scope) == 0 {
		return nil, ErrEmptyScope
	}

	if _, ok := n.GetScope(scope.Name()); ok {
		return nil, ErrScopeExists
	}

	var initRank int64
	if sibling, _ := n.Peek(); sibling != nil {
		initRank = sibling.rank
	}

	newNode := &node[T]{
		parent: n,
		queue:  n.queue,

		rank: initRank,
		id:   n.queue.getNextID(),

		value: &pqueue[T]{scope: scope},
	}

	// Register the scope. We shouldn't *push* the scope onto the heap yet until
	// it has at least one element in it.
	pq := n.pqueue()
	if pq.scopeLookup == nil {
		pq.scopeLookup = make(map[string]*posNode[T])
	}
	pq.scopeLookup[scope.Name()] = &posNode[T]{
		node:  newNode,
		Index: -1, // Start as inactive
	}
	return newNode, nil
}

// CreateValue pushes a value node to n. If v already exists in n, a duplicate
// value entry for v is added. Panics if n is not a scope node.
func (n *node[T]) CreateValue(v T) *node[T] {
	pq := n.pqueue()

	// The initial rank of a new node is the rank of the first child.
	var initRank int64
	if len(pq.children) > 0 {
		initRank = pq.children[0].rank
	}

	newNode := &node[T]{
		parent: n,
		queue:  n.queue,

		rank: initRank,
		id:   n.queue.getNextID(),

		value: v,
	}

	heap.Push(pq, newNode)
	return newNode
}
