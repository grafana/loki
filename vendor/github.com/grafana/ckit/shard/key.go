package shard

import "github.com/cespare/xxhash/v2"

// A Key is used to identify the set of owners for some given objects. Keys
// may be constructed from a string by calling StringKey or by writing data
// through a KeyBuilder.
type Key uint64

// KeyBuilder generate Keys for performing hash lookup. To generate a Key,
// first write to the KeyBuilder, then call Key. The KeyBuilder can be re-used
// afterwards by calling Reset. KeyBuilder can not be used concurrently.
//
// KeyBuilder implements io.Writer.
type KeyBuilder struct {
	dig *xxhash.Digest
}

// NewKeyBuilder returns a new KeyBuilder that can generate keys.
func NewKeyBuilder() *KeyBuilder { return &KeyBuilder{dig: xxhash.New()} }

// Write appends b to kb's state. Write always returns len(b), nil.
func (kb *KeyBuilder) Write(b []byte) (n int, err error) { return kb.dig.Write(b) }

// Reset resets kb's state.
func (kb *KeyBuilder) Reset() { kb.dig.Reset() }

// Key computes the key from kb's current state.
func (kb *KeyBuilder) Key() Key { return Key(kb.dig.Sum64()) }

// StringKey generates a Key directly from a string. It is equivalent to
// writing the input string to a new KeyBuilder.
func StringKey(s string) Key {
	// Don't go through KeyBuilder here; use xxhash.Sum64String directly which is
	// more efficient for this use case but still produces equivalent results.
	return Key(xxhash.Sum64String(s))
}
