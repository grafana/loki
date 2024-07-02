package v2

// Iterator is the basic iterator type with the common functions for advancing
// and retrieving the current value.
//
// General usage of the iterator:
//
//	for it.Next() {
//	    curr := it.At()
//	    // do something
//	}
//	if it.Err() != nil {
//	    // do something
//	}
type Iterator[T any] interface {
	Next() bool
	Err() error
	At() T
}

// Iterators with one single added functionality.

type SizedIterator[T any] interface {
	Iterator[T]
	Remaining() int // remaining
}

type PeekIterator[T any] interface {
	Iterator[T]
	Peek() (T, bool)
}

type SeekIterator[K, V any] interface {
	Iterator[V]
	Seek(K) error
}

type CloseIterator[T any] interface {
	Iterator[T]
	Close() error
}

type CountIterator[T any] interface {
	Iterator[T]
	Count() int
}

type ResetIterator[T any] interface {
	Reset() error
	Iterator[T]
}

// Iterators which are an intersection type of two or more iterators with a
// single added functionality.

type PeekCloseIterator[T any] interface {
	PeekIterator[T]
	CloseIterator[T]
}

type CloseResetIterator[T any] interface {
	CloseIterator[T]
	ResetIterator[T]
}
