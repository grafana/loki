package xcap

import (
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
)

// identifier is an 8-byte unique identifier for captures and regions.
// It is compatible with otel SpanID, sharing the same [8]byte format.
type identifier [8]byte

// ID is an exported alias for identifier.
type ID = identifier

var zeroID identifier

// IsValid reports whether the ID is valid.
func (id identifier) IsValid() bool {
	return id != zeroID
}

// IsZero reports whether the ID is zero (all zeros).
func (id identifier) IsZero() bool {
	return id == zeroID
}

// String returns the hex string representation of the ID.
func (id identifier) String() string {
	return hex.EncodeToString(id[:])
}

// newID returns a new random ID. The ID is guaranteed to be non-zero.
func newID() identifier {
	var id identifier
	for {
		binary.NativeEndian.PutUint64(id[:], rand.Uint64())
		if id.IsValid() {
			break
		}
	}
	return id
}

// NewID returns a new random ID. The ID is guaranteed to be non-zero.
// This is the exported version of newID for use in other packages.
func NewID() ID {
	return newID()
}
