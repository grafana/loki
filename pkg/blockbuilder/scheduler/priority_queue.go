package scheduler

import (
	"container/heap"
)

// PriorityQueue is a generic priority queue with constant time lookups.
type PriorityQueue[T any, K comparable] struct {
	h   *priorityHeap[T]
	m   map[K]*item[T] // Map for constant time lookups
	key func(T) K      // Function to extract key from item
}

// item represents an item in the priority queue with its index
type item[T any] struct {
	value T
	index int
}

// NewPriorityQueue creates a new priority queue.
func NewPriorityQueue[T any, K comparable](less func(T, T) bool, key func(T) K) *PriorityQueue[T, K] {
	h := &priorityHeap[T]{
		less: less,
		heap: make([]*item[T], 0),
		idx:  make(map[int]*item[T]),
	}
	heap.Init(h)
	return &PriorityQueue[T, K]{
		h:   h,
		m:   make(map[K]*item[T]),
		key: key,
	}
}

// Push adds an element to the queue.
func (pq *PriorityQueue[T, K]) Push(v T) {
	k := pq.key(v)
	if existing, ok := pq.m[k]; ok {
		// Update existing item's value and fix heap
		existing.value = v
		heap.Fix(pq.h, existing.index)
		return
	}

	// Add new item
	idx := pq.h.Len()
	it := &item[T]{value: v, index: idx}
	pq.m[k] = it
	heap.Push(pq.h, it)
}

// Pop removes and returns the element with the highest priority from the queue.
func (pq *PriorityQueue[T, K]) Pop() (T, bool) {
	if pq.Len() == 0 {
		var zero T
		return zero, false
	}
	it := heap.Pop(pq.h).(*item[T])
	delete(pq.m, pq.key(it.value))
	return it.value, true
}

// Lookup returns the item with the given key if it exists.
func (pq *PriorityQueue[T, K]) Lookup(k K) (T, bool) {
	if it, ok := pq.m[k]; ok {
		return it.value, true
	}
	var zero T
	return zero, false
}

// Remove removes and returns the item with the given key if it exists.
func (pq *PriorityQueue[T, K]) Remove(k K) (T, bool) {
	it, ok := pq.m[k]
	if !ok {
		var zero T
		return zero, false
	}
	heap.Remove(pq.h, it.index)
	delete(pq.m, k)
	return it.value, true
}

// UpdatePriority updates the priority of an item and reorders the queue.
func (pq *PriorityQueue[T, K]) UpdatePriority(k K, v T) bool {
	if it, ok := pq.m[k]; ok {
		it.value = v
		heap.Fix(pq.h, it.index)
		return true
	}
	return false
}

// Len returns the number of elements in the queue.
func (pq *PriorityQueue[T, K]) Len() int {
	return pq.h.Len()
}

// priorityHeap is the internal heap implementation that satisfies heap.Interface.
type priorityHeap[T any] struct {
	less func(T, T) bool
	heap []*item[T]
	idx  map[int]*item[T] // Maps index to item for efficient updates
}

func (h *priorityHeap[T]) Len() int {
	return len(h.heap)
}

func (h *priorityHeap[T]) Less(i, j int) bool {
	return h.less(h.heap[i].value, h.heap[j].value)
}

func (h *priorityHeap[T]) Swap(i, j int) {
	h.heap[i], h.heap[j] = h.heap[j], h.heap[i]
	h.heap[i].index = i
	h.heap[j].index = j
	h.idx[i] = h.heap[i]
	h.idx[j] = h.heap[j]
}

func (h *priorityHeap[T]) Push(x any) {
	it := x.(*item[T])
	it.index = len(h.heap)
	h.heap = append(h.heap, it)
	h.idx[it.index] = it
}

func (h *priorityHeap[T]) Pop() any {
	old := h.heap
	n := len(old)
	it := old[n-1]
	h.heap = old[0 : n-1]
	delete(h.idx, it.index)
	return it
}

// CircularBuffer is a generic circular buffer.
type CircularBuffer[T any] struct {
	buffer []T
	size   int
	head   int
	tail   int
}

// NewCircularBuffer creates a new circular buffer with the given capacity.
func NewCircularBuffer[T any](capacity int) *CircularBuffer[T] {
	return &CircularBuffer[T]{
		buffer: make([]T, capacity),
		size:   0,
		head:   0,
		tail:   0,
	}
}

// Push adds an element to the circular buffer and returns the evicted element if any
func (b *CircularBuffer[T]) Push(v T) (T, bool) {
	var evicted T
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
func (b *CircularBuffer[T]) Pop() (T, bool) {
	if b.size == 0 {
		var zero T
		return zero, false
	}

	v := b.buffer[b.head]
	b.head = (b.head + 1) % len(b.buffer)
	b.size--

	return v, true
}

// Len returns the number of elements in the buffer
func (b *CircularBuffer[T]) Len() int {
	return b.size
}
