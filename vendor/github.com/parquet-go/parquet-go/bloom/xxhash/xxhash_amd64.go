//go:build !purego

package xxhash

// Sum64 computes the 64-bit xxHash digest of b.
func Sum64(b []byte) uint64
