package xsync

import (
	"math/bits"
	"runtime"
	_ "unsafe"
)

// test-only assert()-like flag
var assertionsEnabled = false

const (
	// cacheLineSize is used in paddings to prevent false sharing;
	// 64B are used instead of 128B as a compromise between
	// memory footprint and performance; 128B usage may give ~30%
	// improvement on NUMA machines.
	cacheLineSize = 64
)

// nextPowOf2 computes the next highest power of 2 of 64-bit v.
func nextPowOf2(v uint64) uint64 {
	if v <= 1 {
		return 1
	}
	return 1 << bits.Len64(v-1)
}

func parallelism() uint32 {
	maxProcs := uint32(runtime.GOMAXPROCS(0))
	numCores := uint32(runtime.NumCPU())
	if maxProcs < numCores {
		return maxProcs
	}
	return numCores
}

//go:noescape
//go:linkname runtime_cheaprand runtime.cheaprand
func runtime_cheaprand() uint32

func broadcast(b uint8) uint64 {
	return 0x101010101010101 * uint64(b)
}

func firstMarkedByteIndex(w uint64) int {
	return bits.TrailingZeros64(w) >> 3
}

// SWAR byte search: may produce false positives, e.g. for 0x0100,
// so make sure to double-check bytes found by this function.
func markZeroBytes(w uint64) uint64 {
	return ((w - 0x0101010101010101) & (^w) & 0x8080808080808080)
}

// Sets byte of the input word at the specified index to the given value.
func setByte(w uint64, b uint8, idx int) uint64 {
	shift := idx << 3
	return (w &^ (0xff << shift)) | (uint64(b) << shift)
}
