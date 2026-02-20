// Package atomicutil provides atomic types that are not available in the
// standard library's sync/atomic package, such as Float64 and Duration.
// It also provides helper constructors for stdlib atomic types that need
// non-zero initialization.
package atomicutil

import (
	"math"
	"sync/atomic"
	"time"
)

// NewBool creates a new atomic.Bool with the given initial value.
func NewBool(val bool) *atomic.Bool {
	b := &atomic.Bool{}
	b.Store(val)
	return b
}

// NewInt32 creates a new atomic.Int32 with the given initial value.
func NewInt32(val int32) *atomic.Int32 {
	i := &atomic.Int32{}
	i.Store(val)
	return i
}

// NewInt64 creates a new atomic.Int64 with the given initial value.
func NewInt64(val int64) *atomic.Int64 {
	i := &atomic.Int64{}
	i.Store(val)
	return i
}

// NewUint32 creates a new atomic.Uint32 with the given initial value.
func NewUint32(val uint32) *atomic.Uint32 {
	u := &atomic.Uint32{}
	u.Store(val)
	return u
}

// NewUint64 creates a new atomic.Uint64 with the given initial value.
func NewUint64(val uint64) *atomic.Uint64 {
	u := &atomic.Uint64{}
	u.Store(val)
	return u
}

// Float64 is an atomic float64 value.
// The zero value is 0.0.
type Float64 struct {
	v atomic.Uint64
}

// NewFloat64 creates a new Float64 with the given initial value.
func NewFloat64(val float64) *Float64 {
	f := &Float64{}
	f.Store(val)
	return f
}

// Load atomically loads and returns the stored float64 value.
func (f *Float64) Load() float64 {
	return math.Float64frombits(f.v.Load())
}

// Store atomically stores val.
func (f *Float64) Store(val float64) {
	f.v.Store(math.Float64bits(val))
}

// CompareAndSwap executes the compare-and-swap operation for the float64 value.
func (f *Float64) CompareAndSwap(old, new float64) bool {
	return f.v.CompareAndSwap(math.Float64bits(old), math.Float64bits(new))
}

// Add atomically adds delta to the stored float64 value and returns the new value.
func (f *Float64) Add(delta float64) float64 {
	for {
		old := f.Load()
		new := old + delta
		if f.CompareAndSwap(old, new) {
			return new
		}
	}
}

// Duration is an atomic time.Duration value.
// The zero value is 0.
type Duration struct {
	v atomic.Int64
}

// NewDuration creates a new Duration with the given initial value.
func NewDuration(val time.Duration) *Duration {
	d := &Duration{}
	d.Store(val)
	return d
}

// Load atomically loads and returns the stored duration value.
func (d *Duration) Load() time.Duration {
	return time.Duration(d.v.Load())
}

// Store atomically stores val.
func (d *Duration) Store(val time.Duration) {
	d.v.Store(int64(val))
}

// Add atomically adds delta to the stored duration value and returns the new value.
func (d *Duration) Add(delta time.Duration) time.Duration {
	return time.Duration(d.v.Add(int64(delta)))
}
