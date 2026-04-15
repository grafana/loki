package bloom

import "github.com/parquet-go/parquet-go/bloom/xxhash"

// Hash is an interface abstracting the hashing algorithm used in bloom filters.
//
// Hash instances must be safe to use concurrently from multiple goroutines.
type Hash interface {
	// Returns the 64 bit hash of the value passed as argument.
	Sum64(value []byte) uint64

	// Compute hashes of individual values of primitive types.
	Sum64Uint8(value uint8) uint64
	Sum64Uint16(value uint16) uint64
	Sum64Uint32(value uint32) uint64
	Sum64Uint64(value uint64) uint64
	Sum64Uint128(value [16]byte) uint64

	// Compute hashes of the array of fixed size values passed as arguments,
	// returning the number of hashes written to the destination buffer.
	MultiSum64Uint8(dst []uint64, src []uint8) int
	MultiSum64Uint16(dst []uint64, src []uint16) int
	MultiSum64Uint32(dst []uint64, src []uint32) int
	MultiSum64Uint64(dst []uint64, src []uint64) int
	MultiSum64Uint128(dst []uint64, src [][16]byte) int
}

// XXH64 is an implementation of the Hash interface using the XXH64 algorithm.
type XXH64 struct{}

func (XXH64) Sum64(b []byte) uint64 {
	return xxhash.Sum64(b)
}

func (XXH64) Sum64Uint8(v uint8) uint64 {
	return xxhash.Sum64Uint8(v)
}

func (XXH64) Sum64Uint16(v uint16) uint64 {
	return xxhash.Sum64Uint16(v)
}

func (XXH64) Sum64Uint32(v uint32) uint64 {
	return xxhash.Sum64Uint32(v)
}

func (XXH64) Sum64Uint64(v uint64) uint64 {
	return xxhash.Sum64Uint64(v)
}

func (XXH64) Sum64Uint128(v [16]byte) uint64 {
	return xxhash.Sum64Uint128(v)
}

func (XXH64) MultiSum64Uint8(h []uint64, v []uint8) int {
	return xxhash.MultiSum64Uint8(h, v)
}

func (XXH64) MultiSum64Uint16(h []uint64, v []uint16) int {
	return xxhash.MultiSum64Uint16(h, v)
}

func (XXH64) MultiSum64Uint32(h []uint64, v []uint32) int {
	return xxhash.MultiSum64Uint32(h, v)
}

func (XXH64) MultiSum64Uint64(h []uint64, v []uint64) int {
	return xxhash.MultiSum64Uint64(h, v)
}

func (XXH64) MultiSum64Uint128(h []uint64, v [][16]byte) int {
	return xxhash.MultiSum64Uint128(h, v)
}

var (
	_ Hash = XXH64{}
)
