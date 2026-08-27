// Package atomicutil provides atomic helpers for types that sync/atomic does
// not wrap (Duration, Float64, String, Error), plus constructors for
// initializing the stdlib typed atomics with a non-zero value.
package atomicutil

import "sync/atomic"

// NewInt32 returns a pointer to an atomic.Int32 holding v.
func NewInt32(v int32) *atomic.Int32 {
	x := new(atomic.Int32)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// NewInt64 returns a pointer to an atomic.Int64 holding v.
func NewInt64(v int64) *atomic.Int64 {
	x := new(atomic.Int64)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// NewUint32 returns a pointer to an atomic.Uint32 holding v.
func NewUint32(v uint32) *atomic.Uint32 {
	x := new(atomic.Uint32)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// NewUint64 returns a pointer to an atomic.Uint64 holding v.
func NewUint64(v uint64) *atomic.Uint64 {
	x := new(atomic.Uint64)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// NewBool returns a pointer to an atomic.Bool holding v.
func NewBool(v bool) *atomic.Bool {
	x := new(atomic.Bool)
	if v {
		x.Store(true)
	}
	return x
}
