package scheduler

import (
	"container/heap"
)

// PriorityQueue is a generic priority queue with constant time lookups.
type PriorityQueue[K comparable, V any] struct {
	h   *priorityHeap[V]
	m   map[K]*item[V] // Map for constant time lookups
	key func(V) K      // Function to extract key from value
}

// item represents an item in the priority queue with its index
type item[V any] struct {
	value V
	index int
}

// NewPriorityQueue creates a new priority queue.
func NewPriorityQueue[K comparable, V any](less func(V, V) bool, key func(V) K) *PriorityQueue[K, V] {
	h := &priorityHeap[V]{
		less: less,
		heap: make([]*item[V], 0),
	}
	heap.Init(h)
	return &PriorityQueue[K, V]{
		h:   h,
		m:   make(map[K]*item[V]),
		key: key,
	}
}

// Push adds an element to the queue.
func (pq *PriorityQueue[K, V]) Push(v V) {
	k := pq.key(v)
	if existing, ok := pq.m[k]; ok {
		// Update existing item's value and fix heap
		existing.value = v
		heap.Fix(pq.h, existing.index)
		return
	}

	// Add new item
	it := &item[V]{value: v}
	pq.m[k] = it
	heap.Push(pq.h, it)
}

// Pop removes and returns the element with the highest priority from the queue.
func (pq *PriorityQueue[K, V]) Pop() (V, bool) {
	if pq.Len() == 0 {
		var zero V
		return zero, false
	}
	it := heap.Pop(pq.h).(*item[V])
	delete(pq.m, pq.key(it.value))
	return it.value, true
}

func (pq *PriorityQueue[K, V]) Peek() (V, bool) {
	if pq.Len() == 0 {
		var zero V
		return zero, false
	}
	return pq.h.heap[0].value, true
}

// Lookup returns the item with the given key if it exists.
func (pq *PriorityQueue[K, V]) Lookup(k K) (V, bool) {
	if it, ok := pq.m[k]; ok {
		return it.value, true
	}
	var zero V
	return zero, false
}

// Remove removes and returns the item with the given key if it exists.
func (pq *PriorityQueue[K, V]) Remove(k K) (V, bool) {
	it, ok := pq.m[k]
	if !ok {
		var zero V
		return zero, false
	}
	heap.Remove(pq.h, it.index)
	delete(pq.m, k)
	return it.value, true
}

// UpdatePriority updates the priority of an item and reorders the queue.
func (pq *PriorityQueue[K, V]) UpdatePriority(k K, v V) bool {
	if it, ok := pq.m[k]; ok {
		it.value = v
		heap.Fix(pq.h, it.index)
		return true
	}
	return false
}

// Len returns the number of elements in the queue.
func (pq *PriorityQueue[K, V]) Len() int {
	return pq.h.Len()
}

// List returns all elements in the queue.
func (pq *PriorityQueue[K, V]) List() []V {
	return pq.h.List()
}

// priorityHeap is the internal heap implementation that satisfies heap.Interface.
type priorityHeap[V any] struct {
	less func(V, V) bool
	heap []*item[V]
}

func (h *priorityHeap[V]) Len() int {
	return len(h.heap)
}

func (h *priorityHeap[V]) Less(i, j int) bool {
	return h.less(h.heap[i].value, h.heap[j].value)
}

func (h *priorityHeap[V]) List() []V {
	vals := make([]V, 0, len(h.heap))
	for _, item := range h.heap {
		vals = append(vals, item.value)
	}
	return vals
}

func (h *priorityHeap[V]) Swap(i, j int) {
	h.heap[i], h.heap[j] = h.heap[j], h.heap[i]
	h.heap[i].index = i
	h.heap[j].index = j
}

func (h *priorityHeap[V]) Push(x any) {
	it := x.(*item[V])
	it.index = len(h.heap)
	h.heap = append(h.heap, it)
}

func (h *priorityHeap[V]) Pop() any {
	old := h.heap
	n := len(old)
	it := old[n-1]
	h.heap = old[0 : n-1]
	return it
}

// CircularBuffer is a generic circular buffer.
type CircularBuffer[V any] struct {
	buffer []V
	size   int
	head   int
	tail   int
}

// NewCircularBuffer creates a new circular buffer with the given capacity.
func NewCircularBuffer[V any](capacity int) *CircularBuffer[V] {
	return &CircularBuffer[V]{
		buffer: make([]V, capacity),
		size:   0,
		head:   0,
		tail:   0,
	}
}

// Push adds an element to the circular buffer and returns the evicted element if any
func (b *CircularBuffer[V]) Push(v V) (V, bool) {
	var evicted V
	hasEvicted := false

	if b.size == len(b.buffer) {
		// If buffer is full, evict the oldest element (at head)
		evicted = b.buffer[b.head]
		hasEvicted = true
		b.head = (b.head + 1) % len(b.buffer)
	} else {
		b.size++
	}

	b.buffer[b.tail] = v
	b.tail = (b.tail + 1) % len(b.buffer)

	return evicted, hasEvicted
}

// Pop removes and returns the oldest element from the buffer
func (b *CircularBuffer[V]) Pop() (V, bool) {
	if b.size == 0 {
		var zero V
		return zero, false
	}

	v := b.buffer[b.head]
	b.head = (b.head + 1) % len(b.buffer)
	b.size--

	return v, true
}

// Len returns the number of elements in the buffer
func (b *CircularBuffer[V]) Len() int {
	return b.size
}

// returns the first element in the buffer that satisfies the given predicate
func (b *CircularBuffer[V]) Lookup(f func(V) bool) (V, bool) {
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % len(b.buffer)
		if f(b.buffer[idx]) {
			return b.buffer[idx], true
		}

	}
	var zero V
	return zero, false
}

// Range iterates over the elements in the buffer from oldest to newest
// and calls the given function for each element.
// If the function returns false, iteration stops.
func (b *CircularBuffer[V]) Range(f func(V) bool) {
	if b.size == 0 {
		return
	}

	// Start from head (oldest) and iterate to tail (newest)
	idx := b.head
	remaining := b.size
	for remaining > 0 {
		if !f(b.buffer[idx]) {
			return
		}
		idx = (idx + 1) % len(b.buffer)
		remaining--
	}
}
