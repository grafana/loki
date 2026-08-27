package atomicutil

import "sync/atomic"

// Error is an atomic error. sync/atomic has no Error wrapper.
// Values are packed so a nil error can be stored without panicking atomic.Value.
type Error struct {
	v atomic.Value
}

type packedError struct{ err error }

// NewError returns a pointer to an Error holding v.
func NewError(v error) *Error {
	x := new(Error)
	if v != nil {
		x.Store(v)
	}
	return x
}

// Load atomically loads the wrapped error.
func (e *Error) Load() error {
	v, ok := e.v.Load().(packedError)
	if !ok {
		return nil
	}
	return v.err
}

// Store atomically stores v.
func (e *Error) Store(v error) {
	e.v.Store(packedError{v})
}

// Swap atomically stores v and returns the previous value.
func (e *Error) Swap(v error) error {
	old, _ := e.v.Swap(packedError{v}).(packedError)
	return old.err
}

// CompareAndSwap atomically compares the current value with old and, if they
// match, stores new. It returns whether the swap was performed.
func (e *Error) CompareAndSwap(old, new error) bool {
	for {
		cur := e.Load()
		if cur != old {
			return false
		}
		// First store: atomic.Value is still nil.
		if e.v.CompareAndSwap(nil, packedError{new}) {
			return true
		}
		if e.v.CompareAndSwap(packedError{old}, packedError{new}) {
			return true
		}
	}
}
