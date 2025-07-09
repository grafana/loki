// Package xxhash is an extension of github.com/cespare/xxhash which adds
// routines optimized to hash arrays of fixed size elements.
package xxhash

import (
	"encoding/binary"
	"math/bits"
)

const (
	prime1 uint64 = 0x9E3779B185EBCA87
	prime2 uint64 = 0xC2B2AE3D27D4EB4F
	prime3 uint64 = 0x165667B19E3779F9
	prime4 uint64 = 0x85EBCA77C2B2AE63
	prime5 uint64 = 0x27D4EB2F165667C5
	// Pre-computed operations because the compiler otherwise complains that the
	// results overflow 64 bit integers.
	prime1plus2 uint64 = 0x60EA27EEADC0B5D6 // prime1 + prime2
	negprime1   uint64 = 0x61C8864E7A143579 // -prime1
)

func avalanche(h uint64) uint64 {
	h ^= h >> 33
	h *= prime2
	h ^= h >> 29
	h *= prime3
	h ^= h >> 32
	return h
}

func round(acc, input uint64) uint64 {
	acc += input * prime2
	acc = rol31(acc)
	acc *= prime1
	return acc
}

func mergeRound(acc, val uint64) uint64 {
	val = round(0, val)
	acc ^= val
	acc = acc*prime1 + prime4
	return acc
}

func u64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func rol1(x uint64) uint64  { return bits.RotateLeft64(x, 1) }
func rol7(x uint64) uint64  { return bits.RotateLeft64(x, 7) }
func rol11(x uint64) uint64 { return bits.RotateLeft64(x, 11) }
func rol12(x uint64) uint64 { return bits.RotateLeft64(x, 12) }
func rol18(x uint64) uint64 { return bits.RotateLeft64(x, 18) }
func rol23(x uint64) uint64 { return bits.RotateLeft64(x, 23) }
func rol27(x uint64) uint64 { return bits.RotateLeft64(x, 27) }
func rol31(x uint64) uint64 { return bits.RotateLeft64(x, 31) }
