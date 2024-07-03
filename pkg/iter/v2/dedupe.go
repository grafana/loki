package v2

// DedupeIter is a deduplicating iterator which creates an Iterator[B]
// from a sequence of Iterator[A].
type DedupeIter[A, B any] struct {
	eq    func(A, B) bool // equality check
	from  func(A) B       // convert A to B, used on first element
	merge func(A, B) B    // merge A into B
	itr   PeekIterator[A]

	tmp B
}

// general helper, in this case created for DedupeIter[T,T]
func Identity[A any](a A) A { return a }

func NewDedupingIter[A, B any](
	eq func(A, B) bool,
	from func(A) B,
	merge func(A, B) B,
	itr PeekIterator[A],
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

// Collect collects an interator into a slice. It uses
// CollectInto with a new slice
func Collect[T any](itr Iterator[T]) ([]T, error) {
	return CollectInto(itr, nil)
}

// CollectInto collects the elements of an iterator into a provided slice
// which is returned
func CollectInto[T any](itr Iterator[T], into []T) ([]T, error) {
	into = into[:0]

	for itr.Next() {
		into = append(into, itr.At())
	}
	return into, itr.Err()
}
