// Copyright 2022 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package rand implements pseudo-random number generators unsuitable for
// security-sensitive work.
//
// Top-level functions that do not have a [Rand] parameter, such as [Float64] and [Int],
// use non-deterministic goroutine-local pseudo-random data sources that produce
// different sequences of values each time a program is run. These top-level functions
// are safe for concurrent use by multiple goroutines, and their performance does
// not degrade when the parallelism increases. [Rand] methods and functions with
// [Rand] parameter are not safe for concurrent use, but should generally be preferred
// because of determinism, higher speed and quality.
//
// This package is considerably faster and generates higher quality random
// than the [math/rand] package. However, this package's outputs might be
// predictable regardless of how it's seeded. For random numbers
// suitable for security-sensitive work, see the [crypto/rand] package.
package rand

import (
	"encoding/binary"
	"io"
	"math"
	"math/bits"
)

const (
	int24Mask = 1<<24 - 1
	int31Mask = 1<<31 - 1
	int53Mask = 1<<53 - 1
	int63Mask = 1<<63 - 1
	intMask   = math.MaxInt

	f24Mul = 0x1.0p-24
	f53Mul = 0x1.0p-53

	randSizeof = 8*4 + 8 + 1
)

// Rand is a pseudo-random number generator based on the [SFC64] algorithm by Chris Doty-Humphrey.
//
// SFC64 has 256 bits of state, average period of ~2^255 and minimum period of at least 2^64.
// Generators returned by [New] (with empty or distinct seeds) are guaranteed
// to not run into each other for at least 2^64 iterations.
//
// [SFC64]: http://pracrand.sourceforge.net/RNG_engines.txt
type Rand struct {
	sfc64
	val uint64
	pos int
}

// New returns an initialized generator. If seed is empty, generator is initialized to a non-deterministic state.
// Otherwise, generator is seeded with the values from seed. New panics if len(seed) > 3.
func New(seed ...uint64) *Rand {
	var r Rand
	r.new_(seed...)
	return &r
}

func (r *Rand) new_(seed ...uint64) {
	switch len(seed) {
	case 0:
		r.init0()
	case 1:
		r.init1(seed[0])
	case 2:
		r.init3(seed[0], seed[1], 0)
	case 3:
		r.init3(seed[0], seed[1], seed[2])
	default:
		panic("invalid New seed sequence length")
	}
}

// Seed uses the provided seed value to initialize the generator to a deterministic state.
func (r *Rand) Seed(seed uint64) {
	r.init1(seed)
	r.val = 0
	r.pos = 0
}

// MarshalBinary returns the binary representation of the current state of the generator.
func (r *Rand) MarshalBinary() ([]byte, error) {
	var data [randSizeof]byte
	r.marshalBinary(&data)
	return data[:], nil
}

func (r *Rand) marshalBinary(data *[randSizeof]byte) {
	binary.LittleEndian.PutUint64(data[0:], r.a)
	binary.LittleEndian.PutUint64(data[8:], r.b)
	binary.LittleEndian.PutUint64(data[16:], r.c)
	binary.LittleEndian.PutUint64(data[24:], r.w)
	binary.LittleEndian.PutUint64(data[32:], r.val)
	data[40] = byte(r.pos)
}

// UnmarshalBinary sets the state of the generator to the state represented in data.
func (r *Rand) UnmarshalBinary(data []byte) error {
	if len(data) < randSizeof {
		return io.ErrUnexpectedEOF
	}
	r.a = binary.LittleEndian.Uint64(data[0:])
	r.b = binary.LittleEndian.Uint64(data[8:])
	r.c = binary.LittleEndian.Uint64(data[16:])
	r.w = binary.LittleEndian.Uint64(data[24:])
	r.val = binary.LittleEndian.Uint64(data[32:])
	r.pos = int(data[40])
	return nil
}

// Float32 returns, as a float32, a uniformly distributed pseudo-random number in the half-open interval [0.0, 1.0).
func (r *Rand) Float32() float32 {
	return float32(r.next32()&int24Mask) * f24Mul
}

// Float64 returns, as a float64, a uniformly distributed pseudo-random number in the half-open interval [0.0, 1.0).
func (r *Rand) Float64() float64 {
	return float64(r.next64()&int53Mask) * f53Mul
}

// Int returns a uniformly distributed non-negative pseudo-random int.
func (r *Rand) Int() int {
	return int(r.next64() & intMask)
}

// Int31 returns a uniformly distributed non-negative pseudo-random 31-bit integer as an int32.
func (r *Rand) Int31() int32 {
	return int32(r.next32() & int31Mask)
}

// Int31n returns, as an int32, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func (r *Rand) Int31n(n int32) int32 {
	if n <= 0 {
		panic("invalid argument to Int31n")
	}
	return int32(r.Uint32n(uint32(n)))
}

// Int63 returns a uniformly distributed non-negative pseudo-random 63-bit integer as an int64.
func (r *Rand) Int63() int64 {
	return int64(r.next64() & int63Mask)
}

// Int63n returns, as an int64, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func (r *Rand) Int63n(n int64) int64 {
	if n <= 0 {
		panic("invalid argument to Int63n")
	}
	return int64(r.Uint64n(uint64(n)))
}

// Intn returns, as an int, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func (r *Rand) Intn(n int) int {
	if n <= 0 {
		panic("invalid argument to Intn")
	}
	if math.MaxInt == math.MaxInt32 {
		return int(r.Uint32n(uint32(n)))
	} else {
		return int(r.Uint64n(uint64(n)))
	}
}

// Perm returns, as a slice of n ints, a pseudo-random permutation of the integers in the half-open interval [0, n).
func (r *Rand) Perm(n int) []int {
	p := make([]int, n)
	r.perm(p)
	return p
}

func (r *Rand) perm(p []int) {
	n := len(p)
	b := n
	if b > math.MaxInt32 {
		b = math.MaxInt32
	}
	i := 1
	for ; i < b; i++ {
		j := r.Uint32n(uint32(i) + 1)
		p[i] = p[j]
		p[j] = i
	}
	for ; i < n; i++ {
		j := r.Uint64n(uint64(i) + 1)
		p[i] = p[j]
		p[j] = i
	}
}

// Read generates len(p) pseudo-random bytes and writes them into p. It always returns len(p) and a nil error.
func (r *Rand) Read(p []byte) (n int, err error) {
	pos := r.pos
	for ; n < len(p) && n < pos; n++ {
		p[n] = byte(r.val)
		r.val >>= 8
		r.pos--
	}
	for ; n+8 <= len(p); n += 8 {
		binary.LittleEndian.PutUint64(p[n:n+8], r.next64())
	}
	if n < len(p) {
		r.val, r.pos = r.next64(), 8
		for ; n < len(p); n++ {
			p[n] = byte(r.val)
			r.val >>= 8
			r.pos--
		}
	}
	return
}

// Shuffle pseudo-randomizes the order of elements. n is the number of elements. Shuffle panics if n < 0.
// swap swaps the elements with indexes i and j.
//
// For shuffling elements of a slice, prefer the top-level [ShuffleSlice] function.
func (r *Rand) Shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("invalid argument to Shuffle")
	}
	i := n - 1
	for ; i > math.MaxInt32-1; i-- {
		j := int(r.Uint64n(uint64(i) + 1))
		swap(i, j)
	}
	for ; i > 0; i-- {
		j := int(r.Uint32n(uint32(i) + 1))
		swap(i, j)
	}
}

// Uint32 returns a uniformly distributed pseudo-random 32-bit value as an uint32.
func (r *Rand) Uint32() uint32 {
	return uint32(r.next32())
}

// next32 has a bit lower inlining cost because of uint64 return value
func (r *Rand) next32() uint64 {
	// unnatural code to fit into inlining budget of 80
	if r.pos < 4 {
		r.val, r.pos = r.next64(), 4
		return r.val >> 32
	} else {
		r.pos = 0
		return r.val
	}
}

// Uint32n returns, as an uint32, a uniformly distributed pseudo-random number in [0, n). Uint32n(0) returns 0.
func (r *Rand) Uint32n(n uint32) uint32 {
	// much faster 32-bit version of Uint64n(); result is unbiased with probability 1 - 2^-32.
	// detecting possible bias would require at least 2^64 samples, which we consider acceptable
	// since it matches 2^64 guarantees about period length and distance between different seeds.
	// note that 2^64 is probably a very conservative estimate: scaled down 16-bit version of this
	// algorithm passes chi-squared test for at least 2^42 (instead of 2^32) values, so
	// 32-bit version will likely require north of 2^80 values to detect non-uniformity.
	res, _ := bits.Mul64(uint64(n), r.next64())
	return uint32(res)
}

// Uint64 returns a uniformly distributed pseudo-random 64-bit value as an uint64.
func (r *Rand) Uint64() uint64 {
	return r.next64()
}

// Uint64n returns, as an uint64, a uniformly distributed pseudo-random number in [0, n). Uint64n(0) returns 0.
func (r *Rand) Uint64n(n uint64) uint64 {
	// "An optimal algorithm for bounded random integers" by Stephen Canon, https://github.com/apple/swift/pull/39143
	res, frac := bits.Mul64(n, r.next64())
	if n <= math.MaxUint32 {
		// we don't use frac <= -n check from the original algorithm, since the branch is unpredictable.
		// instead, we effectively fall back to Uint32n() for 32-bit n
		return res
	}
	hi, _ := bits.Mul64(n, r.next64())
	_, carry := bits.Add64(frac, hi, 0)
	return res + carry
}
