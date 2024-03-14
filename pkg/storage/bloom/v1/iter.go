package v1

type IndexedValue[T any] struct {
	idx int
	val T
}

func (iv IndexedValue[T]) Value() T {
	return iv.val
}

func (iv IndexedValue[T]) Index() int {
	return iv.idx
}

type IterWithIndex[T any] struct {
	Iterator[T]
	zero  T // zero value of T
	cache IndexedValue[T]
}

func (it *IterWithIndex[T]) At() IndexedValue[T] {
	it.cache.val = it.Iterator.At()
	return it.cache
}

func NewIterWithIndex[T any](iter Iterator[T], idx int) Iterator[IndexedValue[T]] {
	return &IterWithIndex[T]{
		Iterator: iter,
		cache:    IndexedValue[T]{idx: idx},
	}
}

type SliceIterWithIndex[T any] struct {
	xs    []T // source slice
	pos   int // position within the slice
	zero  T   // zero value of T
	cache IndexedValue[T]
}

func (it *SliceIterWithIndex[T]) Next() bool {
	it.pos++
	return it.pos < len(it.xs)
}

func (it *SliceIterWithIndex[T]) Err() error {
	return nil
}

func (it *SliceIterWithIndex[T]) At() IndexedValue[T] {
	it.cache.val = it.xs[it.pos]
	return it.cache
}

func (it *SliceIterWithIndex[T]) Peek() (IndexedValue[T], bool) {
	if it.pos+1 >= len(it.xs) {
		it.cache.val = it.zero
		return it.cache, false
	}
	it.cache.val = it.xs[it.pos+1]
	return it.cache, true
}

func NewSliceIterWithIndex[T any](xs []T, idx int) PeekingIterator[IndexedValue[T]] {
	return &SliceIterWithIndex[T]{
		xs:    xs,
		pos:   -1,
		cache: IndexedValue[T]{idx: idx},
	}
}
