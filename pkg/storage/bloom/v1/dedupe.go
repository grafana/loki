package v1

// DedupeIter is a deduplicating iterator which creates an Iterator[B]
// from a sequence of Iterator[A].
type DedupeIter[A, B any] struct {
	eq    func(A, B) bool // equality check
	from  func(A) B       // convert A to B, used on first element
	merge func(A, B) B    // merge A into B
	itr   PeekingIterator[A]

	tmp B
}

// general helper, in this case created for DedupeIter[T,T]
func id[A any](a A) A { return a }

func NewDedupingIter[A, B any](
	eq func(A, B) bool,
	from func(A) B,
	merge func(A, B) B,
	itr PeekingIterator[A],
) *DedupeIter[A, B] {
	return &DedupeIter[A, B]{
		eq:    eq,
		from:  from,
		merge: merge,
		itr:   itr,
	}
}

func (it *DedupeIter[A, B]) Next() bool {
	if !it.itr.Next() {
		return false
	}
	it.tmp = it.from(it.itr.At())
	for {
		next, ok := it.itr.Peek()
		if !ok || !it.eq(next, it.tmp) {
			break
		}

		it.itr.Next() // ensured via peek
		it.tmp = it.merge(next, it.tmp)
	}
	return true
}

func (it *DedupeIter[A, B]) Err() error {
	return it.itr.Err()
}

func (it *DedupeIter[A, B]) At() B {
	return it.tmp
}
