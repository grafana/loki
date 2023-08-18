// Package fastrand implements fast pesudorandom number generator
// that should scale well on multi-CPU systems.
//
// Use crypto/rand instead of this package for generating
// cryptographically secure random numbers.
package fastrand

import (
	"sync"
	"time"
)

// Uint32 returns pseudorandom uint32.
//
// It is safe calling this function from concurrent goroutines.
func Uint32() uint32 {
	v := rngPool.Get()
	if v == nil {
		v = &RNG{}
	}
	r := v.(*RNG)
	x := r.Uint32()
	rngPool.Put(r)
	return x
}

var rngPool sync.Pool

// Uint32n returns pseudorandom uint32 in the range [0..maxN).
//
// It is safe calling this function from concurrent goroutines.
func Uint32n(maxN uint32) uint32 {
	x := Uint32()
	// See http://lemire.me/blog/2016/06/27/a-fast-alternative-to-the-modulo-reduction/
	return uint32((uint64(x) * uint64(maxN)) >> 32)
}

// RNG is a pseudorandom number generator.
//
// It is unsafe to call RNG methods from concurrent goroutines.
type RNG struct {
	x uint32
}

// Uint32 returns pseudorandom uint32.
//
// It is unsafe to call this method from concurrent goroutines.
func (r *RNG) Uint32() uint32 {
	for r.x == 0 {
		r.x = getRandomUint32()
	}

	// See https://en.wikipedia.org/wiki/Xorshift
	x := r.x
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.x = x
	return x
}

// Uint32n returns pseudorandom uint32 in the range [0..maxN).
//
// It is unsafe to call this method from concurrent goroutines.
func (r *RNG) Uint32n(maxN uint32) uint32 {
	x := r.Uint32()
	// See http://lemire.me/blog/2016/06/27/a-fast-alternative-to-the-modulo-reduction/
	return uint32((uint64(x) * uint64(maxN)) >> 32)
}

// Seed sets the r state to n.
func (r *RNG) Seed(n uint32) {
	r.x = n
}

func getRandomUint32() uint32 {
	x := time.Now().UnixNano()
	return uint32((x >> 32) ^ x)
}
