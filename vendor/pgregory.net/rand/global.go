// Copyright 2022 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rand

import (
	"encoding/binary"
	"math"
	"math/bits"
)

// Float32 returns, as a float32, a uniformly distributed pseudo-random number in the half-open interval [0.0, 1.0).
func Float32() float32 {
	return float32(rand64()&int24Mask) * f24Mul
}

// Float64 returns, as a float64, a uniformly distributed pseudo-random number in the half-open interval [0.0, 1.0).
func Float64() float64 {
	return float64(rand64()&int53Mask) * f53Mul
}

// Int returns a uniformly distributed non-negative pseudo-random int.
func Int() int {
	return int(rand64() & intMask)
}

// Int31 returns a uniformly distributed non-negative pseudo-random 31-bit integer as an int32.
func Int31() int32 {
	return int32(rand64() & int31Mask)
}

// Int31n returns, as an int32, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func Int31n(n int32) int32 {
	if n <= 0 {
		panic("invalid argument to Int31n")
	}
	return int32(Uint32n(uint32(n)))
}

// Int63 returns a uniformly distributed non-negative pseudo-random 63-bit integer as an int64.
func Int63() int64 {
	return int64(rand64() & int63Mask)
}

// Int63n returns, as an int64, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func Int63n(n int64) int64 {
	if n <= 0 {
		panic("invalid argument to Int63n")
	}
	return int64(Uint64n(uint64(n)))
}

// Intn returns, as an int, a uniformly distributed non-negative pseudo-random number
// in the half-open interval [0, n). It panics if n <= 0.
func Intn(n int) int {
	if n <= 0 {
		panic("invalid argument to Intn")
	}
	if math.MaxInt == math.MaxInt32 {
		return int(Uint32n(uint32(n)))
	} else {
		return int(Uint64n(uint64(n)))
	}
}

// Perm returns, as a slice of n ints, a pseudo-random permutation of the integers in the half-open interval [0, n).
func Perm(n int) []int {
	p := make([]int, n)
	perm(p)
	return p
}

func perm(p []int) {
	// see Rand.perm
	n := len(p)
	b := n
	if b > math.MaxInt32 {
		b = math.MaxInt32
	}
	i := 1
	for ; i < b; i++ {
		j := Uint32n(uint32(i) + 1)
		p[i] = p[j]
		p[j] = i
	}
	for ; i < n; i++ {
		j := Uint64n(uint64(i) + 1)
		p[i] = p[j]
		p[j] = i
	}
}

// Read generates len(p) pseudo-random bytes and writes them into p. It always returns len(p) and a nil error.
func Read(p []byte) (n int, err error) {
	// see Rand.Read
	for ; n+8 <= len(p); n += 8 {
		binary.LittleEndian.PutUint64(p[n:n+8], rand64())
	}
	if n < len(p) {
		val := rand64()
		for ; n < len(p); n++ {
			p[n] = byte(val)
			val >>= 8
		}
	}
	return
}

// Shuffle pseudo-randomizes the order of elements. n is the number of elements. Shuffle panics if n < 0.
// swap swaps the elements with indexes i and j.
//
// For shuffling elements of a slice, prefer the top-level [ShuffleSlice] function.
func Shuffle(n int, swap func(i, j int)) {
	// see Rand.Shuffle
	if n < 0 {
		panic("invalid argument to Shuffle")
	}
	i := n - 1
	for ; i > math.MaxInt32-1; i-- {
		j := int(Uint64n(uint64(i) + 1))
		swap(i, j)
	}
	for ; i > 0; i-- {
		j := int(Uint32n(uint32(i) + 1))
		swap(i, j)
	}
}

// Uint32 returns a uniformly distributed pseudo-random 32-bit value as an uint32.
func Uint32() uint32 {
	return uint32(rand64())
}

// Uint32n returns, as an uint32, a uniformly distributed pseudo-random number in [0, n). Uint32n(0) returns 0.
func Uint32n(n uint32) uint32 {
	// see Rand.Uint32n
	res, _ := bits.Mul64(uint64(n), rand64())
	return uint32(res)
}

// Uint64 returns a uniformly distributed pseudo-random 64-bit value as an uint64.
func Uint64() uint64 {
	return rand64()
}

// Uint64n returns, as an uint64, a uniformly distributed pseudo-random number in [0, n). Uint64n(0) returns 0.
func Uint64n(n uint64) uint64 {
	// see Rand.Uint64n
	res, frac := bits.Mul64(n, rand64())
	if n <= math.MaxUint32 {
		return res
	}
	hi, _ := bits.Mul64(n, rand64())
	_, carry := bits.Add64(frac, hi, 0)
	return res + carry
}
