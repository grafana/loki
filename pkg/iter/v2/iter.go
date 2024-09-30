package v2

import (
	"context"
	"io"
)

type PeekIter[T any] struct {
	itr Iterator[T]

	// the first call to Next() will populate cur & next
	init      bool
	zero      T // zero value of T for returning empty Peek's
	cur, next *T
}

func NewPeekIter[T any](itr Iterator[T]) *PeekIter[T] {
	return &PeekIter[T]{itr: itr}
}

// populates the first element so Peek can be used and subsequent Next()
// calls will work as expected
func (it *PeekIter[T]) ensureInit() {
	if it.init {
		return
	}
	if it.itr.Next() {
		at := it.itr.At()
		it.next = &at
	}
	it.init = true
}

// load the next element and return the cached one
func (it *PeekIter[T]) cacheNext() {
	it.cur = it.next
	if it.cur != nil && it.itr.Next() {
		at := it.itr.At()
		it.next = &at
	} else {
		it.next = nil
	}
}

func (it *PeekIter[T]) Next() bool {
	it.ensureInit()
	it.cacheNext()
	return it.cur != nil
}

func (it *PeekIter[T]) Peek() (T, bool) {
	it.ensureInit()
	if it.next == nil {
		return it.zero, false
	}
	return *it.next, true
}

func (it *PeekIter[T]) Err() error {
	return it.itr.Err()
}

func (it *PeekIter[T]) At() T {
	return *it.cur
}

type SliceIter[T any] struct {
	cur int
	xs  []T
}

func NewSliceIter[T any](xs []T) *SliceIter[T] {
	return &SliceIter[T]{xs: xs, cur: -1}
}

func (it *SliceIter[T]) Remaining() int {
	return max(0, len(it.xs)-(it.cur+1))
}

func (it *SliceIter[T]) Next() bool {
	it.cur++
	return it.cur < len(it.xs)
}

func (it *SliceIter[T]) Err() error {
	return nil
}

func (it *SliceIter[T]) At() T {
	return it.xs[it.cur]
}

type MapIter[A any, B any] struct {
	Iterator[A]
	f func(A) B
}

func NewMapIter[A any, B any](src Iterator[A], f func(A) B) *MapIter[A, B] {
	return &MapIter[A, B]{Iterator: src, f: f}
}

func (it *MapIter[A, B]) At() B {
	return it.f(it.Iterator.At())
}

type EmptyIter[T any] struct {
	zero T
}

func (it *EmptyIter[T]) Next() bool {
	return false
}

func (it *EmptyIter[T]) Err() error {
	return nil
}

func (it *EmptyIter[T]) At() T {
	return it.zero
}

func (it *EmptyIter[T]) Peek() (T, bool) {
	return it.zero, false
}

func (it *EmptyIter[T]) Remaining() int {
	return 0
}

// noop
func (it *EmptyIter[T]) Reset() {}

func NewEmptyIter[T any]() *EmptyIter[T] {
	return &EmptyIter[T]{}
}

type CancellableIter[T any] struct {
	ctx context.Context
	Iterator[T]
}

func (cii *CancellableIter[T]) Next() bool {
	select {
	case <-cii.ctx.Done():
		return false
	default:
		return cii.Iterator.Next()
	}
}

func (cii *CancellableIter[T]) Err() error {
	if err := cii.ctx.Err(); err != nil {
		return err
	}
	return cii.Iterator.Err()
}

func NewCancelableIter[T any](ctx context.Context, itr Iterator[T]) *CancellableIter[T] {
	return &CancellableIter[T]{ctx: ctx, Iterator: itr}
}

func NewCloserIter[T io.Closer](itr Iterator[T]) *CloserIter[T] {
	return &CloserIter[T]{itr}
}

type CloserIter[T io.Closer] struct {
	Iterator[T]
}

func (i *CloserIter[T]) Close() error {
	return i.At().Close()
}

type PeekCloseIter[T any] struct {
	*PeekIter[T]
	close func() error
}

func NewPeekCloseIter[T any](itr CloseIterator[T]) *PeekCloseIter[T] {
	return &PeekCloseIter[T]{PeekIter: NewPeekIter[T](itr), close: itr.Close}
}

func (it *PeekCloseIter[T]) Close() error {
	return it.close()
}

type Predicate[T any] func(T) bool

func NewFilterIter[T any](it Iterator[T], p Predicate[T]) *FilterIter[T] {
	return &FilterIter[T]{
		Iterator: it,
		match:    p,
	}
}

type FilterIter[T any] struct {
	Iterator[T]
	match Predicate[T]
}

func (i *FilterIter[T]) Next() bool {
	hasNext := i.Iterator.Next()
	for hasNext && !i.match(i.Iterator.At()) {
		hasNext = i.Iterator.Next()
	}
	return hasNext
}

type CounterIter[T any] struct {
	Iterator[T] // the underlying iterator
	count       int
}

func NewCounterIter[T any](itr Iterator[T]) *CounterIter[T] {
	return &CounterIter[T]{Iterator: itr}
}

func (it *CounterIter[T]) Next() bool {
	if it.Iterator.Next() {
		it.count++
		return true
	}
	return false
}

func (it *CounterIter[T]) Count() int {
	return it.count
}

func WithClose[T any](itr Iterator[T], close func() bool) *CloseIter[T] {
	return &CloseIter[T]{
		Iterator: itr,
		close:    close,
	}
}

type CloseIter[T any] struct {
	Iterator[T]
	close func() bool
}

func (i *CloseIter[T]) Close() error {
	if i.close != nil {
		return i.Close()
	}
	return nil
}
