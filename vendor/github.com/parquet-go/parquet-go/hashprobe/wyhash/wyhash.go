// Package wyhash implements a hashing algorithm derived from the Go runtime's
// internal hashing fallback, which uses a variation of the wyhash algorithm.
package wyhash

import (
	"encoding/binary"
	"math/bits"

	"github.com/parquet-go/parquet-go/sparse"
)

const (
	m1 = 0xa0761d6478bd642f
	m2 = 0xe7037ed1a0b428db
	m3 = 0x8ebc6af09c88c6e3
	m4 = 0x589965cc75374cc3
	m5 = 0x1d8e4e27c47d124f
)

func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return hi ^ lo
}

func Hash32(value uint32, seed uintptr) uintptr {
	return uintptr(mix(m5^4, mix(uint64(value)^m2, uint64(value)^uint64(seed)^m1)))
}

func Hash64(value uint64, seed uintptr) uintptr {
	return uintptr(mix(m5^8, mix(value^m2, value^uint64(seed)^m1)))
}

func Hash128(value [16]byte, seed uintptr) uintptr {
	a := binary.LittleEndian.Uint64(value[:8])
	b := binary.LittleEndian.Uint64(value[8:])
	return uintptr(mix(m5^16, mix(a^m2, b^uint64(seed)^m1)))
}

func MultiHash32(hashes []uintptr, values []uint32, seed uintptr) {
	MultiHashUint32Array(hashes, sparse.MakeUint32Array(values), seed)
}

func MultiHash64(hashes []uintptr, values []uint64, seed uintptr) {
	MultiHashUint64Array(hashes, sparse.MakeUint64Array(values), seed)
}

func MultiHash128(hashes []uintptr, values [][16]byte, seed uintptr) {
	MultiHashUint128Array(hashes, sparse.MakeUint128Array(values), seed)
}
