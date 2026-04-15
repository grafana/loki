package v2

type Ord byte

const (
	Less Ord = iota
	Eq
	Greater
)

type Orderable[T any] interface {
	// Return the caller's position relative to the target
	Compare(T) Ord
}

type OrderedImpl[T any] struct {
	val T
	cmp func(T, T) Ord
}

func (o OrderedImpl[T]) Compare(other OrderedImpl[T]) Ord {
	return o.cmp(o.val, other.val)
}

func (o OrderedImpl[T]) Unwrap() T {
	return o.val
}

// convenience method for creating an Orderable implementation
// for a type dynamically by passing in a value and a comparison function
// This is useful for types that are not under our control, such as built-in types
// and for reducing boilerplate in testware/etc.
// Hot-path code should use a statically defined Orderable implementation for performance
func NewOrderable[T any](val T, cmp func(T, T) Ord) OrderedImpl[T] {
	return OrderedImpl[T]{val, cmp}
}

type UnlessIterator[T Orderable[T]] struct {
	a, b PeekIterator[T]
}

// Iterators _must_ be sorted. Defers to underlying `PeekingIterator` implementation
// for both iterators if they implement it.
func NewUnlessIterator[T Orderable[T]](a, b Iterator[T]) *UnlessIterator[T] {
	var peekA, peekB PeekIterator[T]
	var ok bool

	if peekA, ok = a.(PeekIterator[T]); !ok {
		peekA = NewPeekIter(a)
	}

	if peekB, ok = b.(PeekIterator[T]); !ok {
		peekB = NewPeekIter(b)
	}

	return &UnlessIterator[T]{
		a: peekA,
		b: peekB,
	}
}

func (it *UnlessIterator[T]) Next() bool {
outer:
	for it.a.Next() {
		a := it.a.At()

		// advance b until it is greater than or equal to a
		for {
			b, ok := it.b.Peek()
			if !ok {
				// b is empty, so a is not in b
				return true
			}

			switch a.Compare(b) {
			case Less:
				// a is not in b
				return true
			case Eq:
				// a is in b, so continue looking through a
				continue outer
			case Greater:
				// keep advancing b until it is greater than or equal to a
				// no need to check b/c peek ensures we have another
				_ = it.b.Next()
				continue

			}

		}
	}
	return false
}

func (it *UnlessIterator[T]) At() T {
	return it.a.At()
}

func (it *UnlessIterator[T]) Err() error {
	if err := it.a.Err(); err != nil {
		return err
	}
	return it.b.Err()
}
