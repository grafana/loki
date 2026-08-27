package atomicutil

import (
	"sync/atomic"
	"time"
)

// Duration is an atomic time.Duration. sync/atomic has no Duration wrapper.
type Duration struct {
	v atomic.Int64
}

// NewDuration returns a pointer to a Duration holding v.
func NewDuration(v time.Duration) *Duration {
	x := new(Duration)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// Load atomically loads the wrapped duration.
func (d *Duration) Load() time.Duration {
	return time.Duration(d.v.Load())
}

// Store atomically stores v.
func (d *Duration) Store(v time.Duration) {
	d.v.Store(int64(v))
}

// Add atomically adds delta and returns the new value.
func (d *Duration) Add(delta time.Duration) time.Duration {
	return time.Duration(d.v.Add(int64(delta)))
}

// Sub atomically subtracts delta and returns the new value.
func (d *Duration) Sub(delta time.Duration) time.Duration {
	return time.Duration(d.v.Add(-int64(delta)))
}

// Swap atomically stores v and returns the previous value.
func (d *Duration) Swap(v time.Duration) time.Duration {
	return time.Duration(d.v.Swap(int64(v)))
}

// CompareAndSwap atomically compares the current value with old and, if they
// match, stores new. It returns whether the swap was performed.
func (d *Duration) CompareAndSwap(old, new time.Duration) bool {
	return d.v.CompareAndSwap(int64(old), int64(new))
}
