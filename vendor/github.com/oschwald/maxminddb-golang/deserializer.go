package maxminddb

import "math/big"

// deserializer is an interface for a type that deserializes an MaxMind DB
// data record to some other type. This exists as an alternative to the
// standard reflection API.
//
// This is fundamentally different than the Unmarshaler interface that
// several packages provide. A Deserializer will generally create the
// final struct or value rather than unmarshaling to itself.
//
// This interface and the associated unmarshaling code is EXPERIMENTAL!
// It is not currently covered by any Semantic Versioning guarantees.
// Use at your own risk.
type deserializer interface {
	ShouldSkip(offset uintptr) (bool, error)
	StartSlice(size uint) error
	StartMap(size uint) error
	End() error
	String(string) error
	Float64(float64) error
	Bytes([]byte) error
	Uint16(uint16) error
	Uint32(uint32) error
	Int32(int32) error
	Uint64(uint64) error
	Uint128(*big.Int) error
	Bool(bool) error
	Float32(float32) error
}
