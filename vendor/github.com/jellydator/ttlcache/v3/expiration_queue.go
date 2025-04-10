package ttlcache

import (
	"container/heap"
	"container/list"
)

// expirationQueue stores items that are ordered by their expiration
// timestamps. The 0th item is closest to its expiration.
type expirationQueue[K comparable, V any] []*list.Element

// newExpirationQueue creates and initializes a new expiration queue.
func newExpirationQueue[K comparable, V any]() expirationQueue[K, V] {
	q := make(expirationQueue[K, V], 0)
	heap.Init(&q)
	return q
}

// isEmpty checks if the queue is empty.
func (q expirationQueue[K, V]) isEmpty() bool {
	return q.Len() == 0
}

// update updates an existing item's value and position in the queue.
func (q *expirationQueue[K, V]) update(elem *list.Element) {
	heap.Fix(q, elem.Value.(*Item[K, V]).queueIndex)
}

// push pushes a new item into the queue and updates the order of its
// elements.
func (q *expirationQueue[K, V]) push(elem *list.Element) {
	heap.Push(q, elem)
}

// remove removes an item from the queue and updates the order of its
// elements.
func (q *expirationQueue[K, V]) remove(elem *list.Element) {
	heap.Remove(q, elem.Value.(*Item[K, V]).queueIndex)
}

// Len returns the total number of items in the queue.
func (q expirationQueue[K, V]) Len() int {
	return len(q)
}

// Less checks if the item at the i position expires sooner than
// the one at the j position.
func (q expirationQueue[K, V]) Less(i, j int) bool {
	item1, item2 := q[i].Value.(*Item[K, V]), q[j].Value.(*Item[K, V])
	if item1.expiresAt.IsZero() {
		return false
	}

	if item2.expiresAt.IsZero() {
		return true
	}

	return item1.expiresAt.Before(item2.expiresAt)
}

// Swap switches the places of two queue items.
func (q expirationQueue[K, V]) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].Value.(*Item[K, V]).queueIndex = i
	q[j].Value.(*Item[K, V]).queueIndex = j
}

// Push appends a new item to the item slice.
func (q *expirationQueue[K, V]) Push(x interface{}) {
	elem := x.(*list.Element)
	elem.Value.(*Item[K, V]).queueIndex = len(*q)
	*q = append(*q, elem)
}

// Pop removes and returns the last item.
func (q *expirationQueue[K, V]) Pop() interface{} {
	old := *q
	i := len(old) - 1
	elem := old[i]
	elem.Value.(*Item[K, V]).queueIndex = -1
	old[i] = nil // avoid memory leak
	*q = old[:i]

	return elem
}
