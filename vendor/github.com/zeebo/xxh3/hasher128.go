package xxh3

import (
	"hash"
)

// Hasher128 implements the hash.Hash interface.
// It will return hashes that are 128 bits.
type Hasher128 struct {
	h Hasher
}

var (
	_ hash.Hash = (*Hasher128)(nil)
)

// New128 returns a new 128 bit Hasher that implements the hash.Hash interface.
func New128() *Hasher128 {
	return &Hasher128{}
}

// NewSeed128 returns a new Hasher128 that implements the hash.Hash interface.
func NewSeed128(seed uint64) *Hasher128 {
	var h Hasher128
	h.ResetSeed(seed)
	return &h
}

// Reset resets the Hash to its initial state.
func (h *Hasher128) Reset() {
	h.h.Reset()
}

// ResetSeed will reset the hash and set a new seed.
// This will change the original state used by Reset.
func (h *Hasher128) ResetSeed(seed uint64) {
	h.h.ResetSeed(seed)
}

// BlockSize returns the hash's underlying block size.
// The Write method will accept any amount of data, but
// it may operate more efficiently if all writes are a
// multiple of the block size.
func (h *Hasher128) BlockSize() int { return _stripe }

// Size returns the number of bytes Sum will return.
func (h *Hasher128) Size() int { return 16 }

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (h *Hasher128) Sum(b []byte) []byte {
	sum := h.h.Sum128().Bytes()
	return append(b, sum[:]...)
}

// Write adds more data to the running hash.
// It never returns an error.
func (h *Hasher128) Write(buf []byte) (int, error) {
	h.h.update(buf)
	return len(buf), nil
}

// WriteString adds more data to the running hash.
// It never returns an error.
func (h *Hasher128) WriteString(buf string) (int, error) {
	h.h.updateString(buf)
	return len(buf), nil
}

// Sum128 returns the 128-bit hash of the written data.
func (h *Hasher128) Sum128() Uint128 {
	return h.h.Sum128()
}
