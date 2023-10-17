package v1

// MergeDedupeIter is a deduplicating iterator that merges adjacent elements
// It's intended to be used when merging multiple blocks,
// each of which may contain the same fingerprints
type MergeDedupeIter[T any] struct {
	eq    func(T, T) bool
	merge func(T, T) T
	itr   PeekingIterator[T]

	tmp []T
}

func NewMergeDedupingIter[T any](
	eq func(T, T) bool,
	merge func(T, T) T,
	itr PeekingIterator[T],
) *MergeDedupeIter[T] {
	return &MergeDedupeIter[T]{
		eq:    eq,
		merge: merge,
		itr:   itr,
	}
}

func (it *MergeDedupeIter[T]) Next() bool {
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
	for len(it.tmp) > 1 {
		it.tmp[len(it.tmp)-2] = it.merge(it.tmp[len(it.tmp)-2], it.tmp[len(it.tmp)-1])
		it.tmp = it.tmp[:len(it.tmp)-1]
	}
	return true
}

func (it *MergeDedupeIter[T]) Err() error {
	return it.itr.Err()
}

func (it *MergeDedupeIter[T]) At() T {
	return it.tmp[0]
}
