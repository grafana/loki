package topk

import (
	"container/heap"
	"iter"
	"math/rand/v2"
	"slices"
)

// Heap implements a heap of T. If Limit is specified, only the greatest
// elements (according to Less) up to Limit are kept.
//
// When removing elements, the smallest element (according to Less) is returned
// first. If using Heap as a max-heap, these elements need to stored in reverse
// order.
type Heap[T any] struct {
	Limit int               // Maximum number of entries to keep (0 = unlimited). Optional.
	Less  func(a, b T) bool // Less returns true if a < b. Required.

	values []T // Current values in the heap.
}

// Push adds v into the heap. If the heap is full, v is added only if it is
// larger than the smallest value in the heap.
//
// Push returns the result of the operation:
//
// - [PushResultNone] if h is full and v is too small to be added,
// - [PushResultPushed] if h wasn't full and v was added, or
// - [PushResultReplaced] if h was full and v replaced the smallest value.
//
// If Push returns [PushResultReplaced], the previous smallest value is
// returned in prev. Otherwise, prev is the zero value for T.
func (h *Heap[T]) Push(v T) (res PushResult, prev T) {
	if h.Limit == 0 || len(h.values) < h.Limit {
		heap.Push(h.impl(), v)
		return PushResultPushed, prev
	}

	// h.values[0] is always the smallest value in the heap.
	if h.Less(h.values[0], v) {
		prev = heap.Pop(h.impl()).(T)
		heap.Push(h.impl(), v)
		return PushResultReplaced, prev
	}

	return PushResultNone, prev
}

// PushResult describes the result of a [Heap.Push] operation.
type PushResult int

const (
	PushResultNone     PushResult = iota // PushResultNone indicates that the heap was unchanged.
	PushResultPushed                     // PushResultPushed indicates that a value was added without removing existing values.
	PushResultReplaced                   // PushResultReplaced indicates that the smallest value was replaced with a new value.
)

// Pop removes and returns the minimum element from the heap. Pop returns the
// zero value for T and false if the heap is empty.
func (h *Heap[T]) Pop() (T, bool) {
	if len(h.values) == 0 {
		var zero T
		return zero, false
	}

	return heap.Pop(heapImpl[T]{h}).(T), true
}

// Len returns the current number of elements in the heap.
func (h *Heap[T]) Len() int { return len(h.values) }

// PopAll removes and returns all elements from the heap in sorted order.
func (h *Heap[T]) PopAll() []T {
	res := h.values

	slices.SortFunc(res, func(a, b T) int {
		if h.Less(a, b) {
			return -1
		}
		return 1
	})

	// Reset h.values to nil to avoid changes to the heap modifying the returned
	// slice.
	h.values = nil
	return res
}

// Range returns an iterator over elements in the heap in random order without
// modifying the heap. The iteration order is not consistent between calls to
// Range.
//
// To retrieve items in sorted order, use [Heap.Pop] or [Heap.PopAll].
func (h *Heap[T]) Range() iter.Seq[T] {
	if len(h.values) == 0 {
		return func(func(T) bool) {}
	}

	// Create a random start point in the heap to avoid relying on the return
	// order.
	//
	// This is similar to how Go range over maps work, but that creates a seed at
	// the time the heap is created rather than when ranging begins.
	start := rand.IntN(len(h.values))

	return func(yield func(T) bool) {
		curr := start

		for {
			if !yield(h.values[curr]) {
				return
			}

			// Increment curr and stop once we've fully looped back to where we
			// started.
			curr = (curr + 1) % len(h.values)
			if curr == start {
				return
			}
		}
	}
}

type heapImpl[T any] struct {
	*Heap[T]
}

func (h *Heap[T]) impl() heap.Interface { return heapImpl[T]{h} }

var _ heap.Interface = (*heapImpl[int])(nil)

func (impl heapImpl[T]) Len() int { return impl.Heap.Len() }

func (impl heapImpl[T]) Less(i, j int) bool {
	return impl.Heap.Less(impl.values[i], impl.values[j])
}

func (impl heapImpl[T]) Swap(i, j int) {
	impl.values[i], impl.values[j] = impl.values[j], impl.values[i]
}

func (impl heapImpl[T]) Push(x any) {
	impl.values = append(impl.values, x.(T))
}

func (impl heapImpl[T]) Pop() any {
	old := impl.values
	n := len(old)
	x := old[n-1]
	impl.values = old[:n-1]
	return x
}
