package atomicutil

import (
	"math"
	"sync/atomic"
)

// Float64 is an atomic float64. sync/atomic has no Float64 wrapper.
type Float64 struct {
	v atomic.Uint64
}

// NewFloat64 returns a pointer to a Float64 holding v.
func NewFloat64(v float64) *Float64 {
	x := new(Float64)
	if v != 0 {
		x.Store(v)
	}
	return x
}

// Load atomically loads the wrapped float64.
func (f *Float64) Load() float64 {
	return math.Float64frombits(f.v.Load())
}

// Store atomically stores v.
func (f *Float64) Store(v float64) {
	f.v.Store(math.Float64bits(v))
}

// Swap atomically stores v and returns the previous value.
func (f *Float64) Swap(v float64) float64 {
	return math.Float64frombits(f.v.Swap(math.Float64bits(v)))
}

// CompareAndSwap atomically compares the current value with old and, if they
// match, stores new. It returns whether the swap was performed.
//
// NaN is treated as equal to NaN so CAS loops cannot livelock, matching
// go.uber.org/atomic.
func (f *Float64) CompareAndSwap(old, new float64) bool {
	return f.v.CompareAndSwap(math.Float64bits(old), math.Float64bits(new))
}

// Add atomically adds delta and returns the new value.
func (f *Float64) Add(delta float64) float64 {
	for {
		old := f.Load()
		new := old + delta
		if f.CompareAndSwap(old, new) {
			return new
		}
	}
}

// Sub atomically subtracts delta and returns the new value.
func (f *Float64) Sub(delta float64) float64 {
	return f.Add(-delta)
}
