package shard

import (
	"encoding/binary"
	"fmt"
)

const (
	// AlgorithmFirstByte is the first-byte shard algorithm: int(ngram[0]) % shardCount.
	AlgorithmFirstByte = "first_byte"

	// AlgorithmMurmur3Mix uses the murmur3 64-bit finalizer over the full 8-byte
	// n-gram key. Branch-free, no loop, excellent avalanche — every input bit
	// affects every output bit. Replaces first_byte for new indexes.
	AlgorithmMurmur3Mix = "murmur3_mix"
)

// Func computes a shard index for an n-gram given a shard count.
type Func func(ngram [8]byte, count int) int

// New returns a Func for the given algorithm name.
// Empty algorithm returns a noop function (always returns 0).
// Unknown algorithm returns an error.
func New(algorithm string) (Func, error) {
	switch algorithm {
	case AlgorithmFirstByte:
		return FirstByte, nil
	case AlgorithmMurmur3Mix:
		return Murmur3Mix, nil
	case "":
		return Noop, nil
	default:
		return nil, fmt.Errorf("unknown shard algorithm %q", algorithm)
	}
}

// Noop always returns shard 0.
func Noop(_ [8]byte, _ int) int { return 0 }

// FirstByte routes by int(ngram[0]) % count.
func FirstByte(ngram [8]byte, count int) int {
	return int(ngram[0]) % count
}

// Murmur3Mix applies the murmur3 64-bit finalizer to the n-gram key interpreted
// as a little-endian uint64. Three XOR-shifts and two multiplies — no loop,
// no branching, no dependencies.
func Murmur3Mix(ngram [8]byte, count int) int {
	v := binary.LittleEndian.Uint64(ngram[:])
	v ^= v >> 33
	v *= 0xff51afd7ed558ccd
	v ^= v >> 33
	v *= 0xc4ceb9fe1a85ec53
	v ^= v >> 33
	return int(v % uint64(count))
}
