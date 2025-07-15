// Package result provides utilities for dealing with iterators that can fail
// during iteration.
//
// Result is useful to make it harder for callers to ignore errors. Using
// iter.Seq2[V, error] can make it easy to accidentally ignore errors:
//
//	func myIter() iter.Seq2[V, error] { ... }
//
//	func main() {
//	  for v := range myIter() { /* errors are ignored! */ }
//	}
package result

import (
	"errors"
	"iter"
)

// Result is a type used for representing a result from an operation that can
// fail.
type Result[V any] struct {
	value V // Valid only if err is nil.
	err   error
}

// Value returns a successful result with the given value.
func Value[V any](v V) Result[V] {
	return Result[V]{value: v}
}

// Error returns a failed result with the given error.
func Error[V any](err error) Result[V] {
	return Result[V]{err: err}
}

// Value returns r's value and error.
func (r Result[V]) Value() (V, error) {
	return r.value, r.err
}

// MustValue returns r's value. If r is an error, MustValue panics.
func (r Result[V]) MustValue() V {
	if r.err != nil {
		panic(r.err)
	}
	return r.value
}

// Err returns r's error, if any.
func (r Result[V]) Err() error {
	return r.err
}

// Seq is an iterator over sequences of result values. When called as
// seq(yield), seq calls yield(r) for each value r in the sequence, stopping
// early if yield returns false.
//
// See the [iter] package for more information on iterators.
type Seq[V any] func(yield func(Result[V]) bool)

// Iter produces a new Seq[V] from a given function that can fail. Values
// passed to yield are wrapped in a call to [Value], while a non-nil error is
// wrapped in a call to [Error].
//
// Iter makes it easier to write failable iterators and removes the need to
// manually wrap values and errors into a [Result].
func Iter[V any](seq func(yield func(V) bool) error) Seq[V] {
	return func(yield func(Result[V]) bool) {
		err := seq(func(v V) bool { return yield(Value(v)) })
		if err != nil {
			yield(Error[V](err))
		}
	}
}

// Pull converts the "push-style" Result iterator sequence seq into a
// "pull-style" iterator accessed by the two functions next and stop.
//
// Pull is a wrapper around [iter.Pull].
func Pull[V any](seq Seq[V]) (next func() (Result[V], bool), stop func()) {
	iseq := iter.Seq[Result[V]](seq)
	return iter.Pull(iseq)
}

// Collect collects values from seq into a new slice and returns it. Any errors
// from seq are joined and returned as the second value.
func Collect[V any](seq Seq[V]) ([]V, error) {
	var (
		vals []V
		errs []error
	)
	for res := range seq {
		val, err := res.Value()
		if err != nil {
			errs = append(errs, err)
		} else {
			vals = append(vals, val)
		}
	}
	return vals, errors.Join(errs...)
}
