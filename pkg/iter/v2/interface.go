package v2

type Iterator[T any] interface {
	Next() bool
	Err() error
	At() T
}

type SizedIterator[T any] interface {
	Iterator[T]
	Remaining() int // remaining
}

type PeekingIterator[T any] interface {
	Peek() (T, bool)
	Iterator[T]
}

type SeekIter[K, V any] interface {
	Seek(K) error
	Iterator[V]
}

type CloseableIterator[T any] interface {
	Iterator[T]
	Close() error
}

type CounterIterator[T any] interface {
	Iterator[T]
	Count() int
}
