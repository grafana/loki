package v1

// DedupeIter is a deduplicating iterator that merges adjacent elements
// It's intended to be used when merging multiple blocks,
// each of which may contain the same fingerprints
type DedupeIter[T any] struct {
	eq    func(T, T) bool
	merge func(T, T) T
	itr   PeekingIterator[T]

	tmp []T
}

func NewDedupingIter[T any](
	eq func(T, T) bool,
	merge func(T, T) T,
	itr PeekingIterator[T],
) *DedupeIter[T] {
	return &DedupeIter[T]{
		eq:    eq,
		merge: merge,
		itr:   itr,
	}
}

func (it *DedupeIter[T]) Next() bool {
	it.tmp = it.tmp[:0]
	if !it.itr.Next() {
		return false
	}
	it.tmp = append(it.tmp, it.itr.At())
	for {
		next, ok := it.itr.Peek()
		if !ok || !it.eq(it.tmp[0], next) {
			break
		}

		it.itr.Next() // ensured via peek
		it.tmp = append(it.tmp, it.itr.At())
	}

	// merge all the elements in tmp
	for i := len(it.tmp) - 1; i > 0; i-- {
		it.tmp[i-1] = it.merge(it.tmp[i-1], it.tmp[i])
	}
	return true
}

func (it *DedupeIter[T]) Err() error {
	return it.itr.Err()
}

func (it *DedupeIter[T]) At() T {
	return it.tmp[0]
}
