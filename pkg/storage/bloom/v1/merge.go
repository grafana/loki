package v1

import (
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

// HeapIterator is a heap implementation of BlockQuerier backed by multiple blocks
// It is used to merge multiple blocks into a single ordered querier
// NB(owen-d): it uses a custom heap implementation because Pop() only returns a single
// value of the top-most iterator, rather than the iterator itself
type HeapIterator[T any] struct {
	itrs []iter.PeekIterator[T]
	less func(T, T) bool

	zero  T // zero value of T
	cache T
	ok    bool
}

func NewHeapIterForSeriesWithBloom(queriers ...iter.PeekIterator[*SeriesWithBlooms]) *HeapIterator[*SeriesWithBlooms] {
	return NewHeapIterator(
		func(a, b *SeriesWithBlooms) bool {
			return a.Series.Fingerprint < b.Series.Fingerprint
		},
		queriers...,
	)
}

func NewHeapIterator[T any](less func(T, T) bool, itrs ...iter.PeekIterator[T]) *HeapIterator[T] {
	res := &HeapIterator[T]{
		itrs: itrs,
		less: less,
	}
	res.init()
	return res
}

func (mbq HeapIterator[T]) Len() int {
	return len(mbq.itrs)
}

func (mbq *HeapIterator[T]) Less(i, j int) bool {
	a, aOk := mbq.itrs[i].Peek()
	b, bOk := mbq.itrs[j].Peek()
	if !bOk {
		return true
	}
	if !aOk {
		return false
	}
	return mbq.less(a, b)
}

func (mbq *HeapIterator[T]) Swap(a, b int) {
	mbq.itrs[a], mbq.itrs[b] = mbq.itrs[b], mbq.itrs[a]
}

func (mbq *HeapIterator[T]) Next() (ok bool) {
	mbq.cache, ok = mbq.pop()
	return
}

// TODO(owen-d): don't swallow this error
func (mbq *HeapIterator[T]) Err() error {
	return nil
}

func (mbq *HeapIterator[T]) At() T {
	return mbq.cache
}

func (mbq *HeapIterator[T]) pop() (T, bool) {
	for {
		if mbq.Len() == 0 {
			return mbq.zero, false
		}

		cur := mbq.itrs[0]
		if ok := cur.Next(); !ok {
			mbq.remove(0)
			continue
		}

		result := cur.At()

		_, ok := cur.Peek()
		if !ok {
			// that was the end of the iterator. remove it from the heap
			mbq.remove(0)
		} else {
			_ = mbq.down(0)
		}

		return result, true
	}
}

func (mbq *HeapIterator[T]) remove(idx int) {
	mbq.Swap(idx, mbq.Len()-1)
	mbq.itrs[len(mbq.itrs)-1] = nil // don't leak reference
	mbq.itrs = mbq.itrs[:mbq.Len()-1]
	mbq.fix(idx)
}

// fix re-establishes the heap ordering after the element at index i has changed its value.
// Changing the value of the element at index i and then calling fix is equivalent to,
// but less expensive than, calling Remove(h, i) followed by a Push of the new value.
// The complexity is O(log n) where n = h.Len().
func (mbq *HeapIterator[T]) fix(i int) {
	if !mbq.down(i) {
		mbq.up(i)
	}
}

func (mbq *HeapIterator[T]) up(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !mbq.Less(j, i) {
			break
		}
		mbq.Swap(i, j)
		j = i
	}
}

func (mbq *HeapIterator[T]) down(i0 int) (moved bool) {
	i := i0
	n := mbq.Len()
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && mbq.Less(j2, j1) {
			j = j2 // take the higher priority child index
		}
		if !mbq.Less(j, i) {
			break
		}
		mbq.Swap(i, j)
		i = j
	}

	return i > i0
}

// establish heap invariants. O(n)
func (mbq *HeapIterator[T]) init() {
	n := mbq.Len()
	for i := n/2 - 1; i >= 0; i-- {
		_ = mbq.down(i)
	}
}
