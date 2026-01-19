// Package fair implements a Hierarchical Fair Queue (HFQ), providing balanced
// service across a hierarchy of queues.
package fair

import (
	"errors"
	"fmt"
)

var (
	// ErrEmptyScope is returned when an empty scope name is provided.
	ErrEmptyScope = errors.New("missing scope name")

	// ErrScopeExists is returned by [Queue.RegisterScope] when a scope is already
	// registered.
	ErrScopeExists = errors.New("scope already exists")

	// ErrNotFound is returned when a scope or value is not found.
	ErrNotFound = errors.New("not found")
)

// Queue is a hierarchical fair priority queue. It is composed of a tree of
// [Scope]s, where each scope holds other scopes or enqueued items.
//
// Scopes must be defined with [Queue.RegisterScope] before they can be used.
// When a scope is no longer needed, call [Queue.UnregisterScope] to remove it.
//
// Scopes have a "rank" which determines their priority for [Queue.Pop], with a
// lower rank indicating higher priority. When two ranks are equal, priority is
// given to the scope or value added first.
//
// Call [Queue.AdjustScope] on a scope to adjust its rank after consuming a value.
// The rank adjustment propagates up the tree to the root scope, giving priority
// to other scopes, ensuring fairness across the hierarchy.
//
// The zero value for Queue is ready for use.
type Queue[T any] struct {
	nextID int64 // ID used for creating nodes.

	len  int
	root node[T]
}

// Len returns the number of items in the queue.
func (q *Queue[T]) Len() int { return q.len }

func (q *Queue[T]) init() {
	if q.root.value == nil {
		q.root.queue = q
		q.root.value = &pqueue[T]{}
	}
}

// RegisterScope registers a new scope, automatically registering intermediate
// scopes if they do not yet exist.
//
// RegisterScope returns an error if the full scope is already registered.
func (q *Queue[T]) RegisterScope(scope Scope) error {
	q.init()

	if len(scope) == 0 {
		return ErrEmptyScope
	}

	cur := &q.root
	for i := range len(scope) - 1 {
		node, ok := cur.GetScope(scope[i])
		if !ok {
			node, _ = cur.RegisterScope(scope[:i+1])
		}
		cur = node
	}

	_, err := cur.RegisterScope(scope)
	return err
}

// UnregisterScope unregisters the provided scope. If the scope was the only
// child of its parent, the parent is also unregistered (recursively up to the
// root scope).
//
// UnregisterScope returns [ErrNotFound] if the scope does not exist.
func (q *Queue[T]) UnregisterScope(scope Scope) error {
	q.init()

	if len(scope) == 0 {
		return ErrEmptyScope
	}

	node, err := q.findScope(scope)
	if err != nil {
		return err
	}
	q.len -= scopeItems(node)

	// There are two passes we want to do:
	//
	// 1. Recursively tombstone the scope and its inactive parents.
	// 2. Recursively unregister the scope and its empty parents.
	//
	// Both must be done; it's possible that we may tombstone the entirety of
	// the scope tree but we're only able to unregister a subset of it. Doing
	// both ensures that we don't keep a subtree alive when it has no children.
	q.tombstoneScope(node)

	for node.parent != nil {
		parent := node.parent
		scopeName := node.Scope().Name()

		parent.UnregisterScope(scopeName)
		if parent.Len() != 0 || parent.NumChildren() != 0 {
			break
		}
		node = parent
	}

	return nil
}

// scopeItems recursively counts the number of items in the subtree rooted at n.
func scopeItems[T any](n *node[T]) int {
	pq := n.pqueue()

	var total int
	for _, c := range pq.children {
		switch v := c.value.(type) {
		case T:
			total++
		case *pqueue[T]:
			total += scopeItems(c.node)
		default:
			panic(fmt.Sprintf("unexpected value type %T", v))
		}
	}
	return total
}

func (q *Queue[T]) findScope(s Scope) (*node[T], error) {
	if len(s) == 0 {
		return nil, ErrEmptyScope
	}

	cur := &q.root

	for i := range s {
		node, ok := cur.GetScope(s[i])
		if !ok {
			return nil, ErrNotFound
		} else if len(s) == i+1 {
			// This was the last element in the path, so we found the node.
			return node, nil
		}

		cur = node
	}

	return nil, ErrNotFound
}

// Push enqueues the value at the specified scope.
//
// The value is assigned the minimum rank among its siblings to prevent it from
// jumping the line. If no siblings exist, the rank is set to 0.
//
// Push returns [ErrNotFound] if the scope does not exist.
func (q *Queue[T]) Push(scope Scope, value T) error {
	q.init()

	parent, err := q.findScope(scope)
	if err != nil {
		return err
	}

	_ = parent.CreateValue(value)

	// Now that our scope has a value, we need to make sure that our scope is
	// present in the pqueue heap of all its parent (recursively).
	q.markAlive(parent)

	q.len++
	return nil
}

// markAlive marks the provided scopeNode as alive, allowing it to be traversed
// from the root.
//
// markAlive must only be called when the scopeNode has a value.
func (q *Queue[T]) markAlive(scopeNode *node[T]) {
	if scopeNode.Len() == 0 {
		panic("markAlive called on scopeNode without value")
	}

	cur := scopeNode
	for cur.parent != nil {
		parent := cur.parent
		parent.MarkAlive(cur.Scope().Name())

		cur = parent
	}
}

// Peek returns the next value with the highest priority along with its scope.
// The value is not removed from the queue.
//
// See [Queue.Pop] for information on how the next highest priority is
// determined.
//
// If q is empty, Peek returns the zero value for T and a nil Scope.
func (q *Queue[T]) Peek() (T, Scope) {
	q.init()

	cur := &q.root
	for {
		if cur.Len() == 0 {
			var zero T
			return zero, nil
		}

		minValue, scope := cur.Peek()
		switch k := minValue.value.(type) {
		case T:
			return k, scope
		case *pqueue[T]:
			cur = minValue
		default:
			panic(fmt.Sprintf("unexpected value type %T", k))
		}
	}
}

// Pop removes and returns the next value with the highest priority along with
// its scope.
//
// Priority is determined by traversing the scope tree, selecting the scope with
// the lowest rank at each traversal, followed by the lowest rank of the
// children of the selected scope.
//
// Popping a value does not adjust the rank of the scope tree. To maintain
// fairness, call [Queue.AdjustScope] with the returned scope to adjust its priority
// based on some cost of the returned value.
//
// If q is empty, Pop returns the zero value for T and a nil path.
func (q *Queue[T]) Pop() (T, Scope) {
	q.init()

	cur := &q.root

	for {
		if cur.Len() == 0 {
			var zero T
			return zero, nil
		}

		// We don't want to Pop yet: we want to Pop the queued element,
		// but intermediate scopes should only be popped when t
		minValue, scope := cur.Peek()
		switch k := minValue.value.(type) {
		case T:
			_, _ = cur.Pop()

			if cur.Len() == 0 {
				// We just removed the final child, so now we need to tombstone
				// the scope. If we don't do this, the scope will continue being
				// selected during traversal, despite it having no values.
				q.tombstoneScope(cur)
			}

			q.len--
			return k, scope
		case *pqueue[T]:
			cur = minValue
		default:
			panic(fmt.Sprintf("unexpected value type %T", k))
		}
	}
}

// tombstoneScope removes an empty scope node from its parent scope.
//
// This does *not* delete scopes, just removes them from the heap.
func (q *Queue[T]) tombstoneScope(scopeNode *node[T]) {
	// Recursively remove scopes from their parents if they've become empty.
	//
	// NOTE(rfratto): This could be rewritten as calling Pop on each parent
	// (since we only found the scope to tombstone by traversing in priority
	// order), but since we use MarkDead elsewhere it's kept here for
	// consistency and ease of understanding.
	for scopeNode.parent != nil {
		scopeName := scopeNode.Scope().Name()
		scopeNode.parent.MarkDead(scopeName)
		if scopeNode.parent.Len() != 0 {
			break
		}

		scopeNode = scopeNode.parent
	}
}

// AdjustScope traverses scope, modifying the rank of each layer by the given cost.
//
// cost can be positive (to decrease the priority of scopes) or negative (to
// increase the priority).
//
// AdjustScope returns an error if the scope does not exist.
func (q *Queue[T]) AdjustScope(scope Scope, cost int64) error {
	q.init()

	node, err := q.findScope(scope)
	if err != nil {
		return err
	}

	// Traverse all the way up to the root, applying cost.
	for node != nil {
		node.rank += cost

		// If node has a parent, we need to fix the child's position since we
		// just modified its rank.
		if node.parent != nil {
			node.parent.FixChild(node.Scope().Name())
		}
		node = node.parent
	}
	return nil
}

// getNextID returns the next ID to be used for creating a node. This can be
// called by [node].
func (q *Queue[T]) getNextID() int64 {
	id := q.nextID
	q.nextID++
	return id
}
