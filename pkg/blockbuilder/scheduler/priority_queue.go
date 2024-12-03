package scheduler

import (
	"container/heap"
)

// PriorityQueue is a generic priority queue.
type PriorityQueue[T any] struct {
	h *priorityHeap[T]
}

// NewPriorityQueue creates a new priority queue.
func NewPriorityQueue[T any](less func(T, T) bool) *PriorityQueue[T] {
	h := &priorityHeap[T]{
		less: less,
		heap: make([]T, 0),
	}
	heap.Init(h)
	return &PriorityQueue[T]{h: h}
}

// Push adds an element to the queue.
func (pq *PriorityQueue[T]) Push(v T) {
	heap.Push(pq.h, v)
}

// Pop removes and returns the element with the highest priority from the queue.
func (pq *PriorityQueue[T]) Pop() (T, bool) {
	if pq.Len() == 0 {
		var zero T
		return zero, false
	}
	return heap.Pop(pq.h).(T), true
}

// Len returns the number of elements in the queue.
func (pq *PriorityQueue[T]) Len() int {
	return pq.h.Len()
}

// priorityHeap is the internal heap implementation that satisfies heap.Interface.
type priorityHeap[T any] struct {
	less func(T, T) bool
	heap []T
}

func (h *priorityHeap[T]) Len() int {
	return len(h.heap)
}

func (h *priorityHeap[T]) Less(i, j int) bool {
	return h.less(h.heap[i], h.heap[j])
}

func (h *priorityHeap[T]) Swap(i, j int) {
	h.heap[i], h.heap[j] = h.heap[j], h.heap[i]
}

func (h *priorityHeap[T]) Push(x any) {
	h.heap = append(h.heap, x.(T))
}

func (h *priorityHeap[T]) Pop() any {
	old := h.heap
	n := len(old)
	x := old[n-1]
	h.heap = old[0 : n-1]
	return x
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

// Push adds an element to the circular buffer.
func (cb *CircularBuffer[T]) Push(v T) {
	if cb.size == len(cb.buffer) {
		cb.head = (cb.head + 1) % len(cb.buffer)
	} else {
		cb.size++
	}
	cb.buffer[cb.tail] = v
	cb.tail = (cb.tail + 1) % len(cb.buffer)
}

// Pop removes and returns the oldest element from the circular buffer.
func (cb *CircularBuffer[T]) Pop() (T, bool) {
	if cb.size == 0 {
		var zero T
		return zero, false
	}
	v := cb.buffer[cb.head]
	cb.head = (cb.head + 1) % len(cb.buffer)
	cb.size--
	return v, true
}

// Len returns the number of elements in the circular buffer.
func (cb *CircularBuffer[T]) Len() int {
	return cb.size
}
