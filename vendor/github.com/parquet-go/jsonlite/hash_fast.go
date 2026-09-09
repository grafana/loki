//go:build amd64 || 386 || arm64 || ppc64 || ppc64le || loong64 || s390x || wasm

package jsonlite

import (
	"math/bits"
	"math/rand/v2"
	"unsafe"
)

// hashseed randomizes the tag bytes across processes, so the key sets that
// collide cannot be worked out ahead of time from a published document.
//
// A single multiply is weaker protection than maphash: an attacker who
// recovers the seed can force every key in an object onto one tag. The damage
// is bounded, since a fully collided index degrades to the linear key scan
// small objects already perform, but it is weaker.
var hashseed = rand.Uint64()

// hashKey produces the 1-byte tag stored in an object's hash index.
//
// Only one byte of the hash is ever consumed, so a general-purpose 64-bit hash
// is heavily over-provisioned for the job: this folds the first and last 8
// bytes of the key into one word and mixes it with a single multiply, taking
// the top byte of the product. Every input bit reaches that byte, which is all
// the entropy a 1-byte tag can use. Cost is independent of key length, and
// both loads stay inside the key's own bytes.
//
// The shape is deliberately kept under the inliner's budget (cost 75 of 80).
// At roughly 2ns a call the call overhead alone would be a third of the cost,
// so adding a tier here, or richer mixing in the n<4 case, costs more than it
// recovers. If a change to this function pushes it over the budget the win
// disappears; `go build -gcflags=-m=2` reports the cost.
//
// The two loads are raw unaligned dereferences, which is what keeps the
// function inlinable: routing them through binary.LittleEndian, the portable
// idiom the runtime uses in readUnaligned64, measures at cost 111 and does
// not inline. So this build is constrained to the architectures the compiler
// marks unalignedOK in cmd/compile/internal/ssa/config.go; arm, mips, mipsle,
// mips64, mips64le and riscv64 take the maphash fallback in hash_generic.go.
//
// The loads are native-endian, so a big-endian build computes different tags
// than a little-endian one. That is harmless: a tag is only ever compared
// against another tag produced by the same process.
//
// 386 qualifies on alignment but is the one 32-bit member of the set, and the
// 64-bit arithmetic below costs enough there to miss the inline budget
// (cost 93), so 386 gets this hash out of line and with emulated 64-bit ops.
// Whether that still beats maphash on 386 is unmeasured; if it does not, 386
// is the one entry worth moving to hash_generic.go.
func hashKey(k string) byte {
	n := len(k)
	p := unsafe.Pointer(unsafe.StringData(k))
	var x uint64
	switch {
	case n >= 8:
		x = *(*uint64)(p) ^ bits.RotateLeft64(*(*uint64)(unsafe.Add(p, n-8)), 32)
	case n >= 4:
		x = uint64(*(*uint32)(p)) | uint64(*(*uint32)(unsafe.Add(p, n-4)))<<32
	case n > 0:
		x = uint64(*(*byte)(p))
	}
	return byte(((x ^ (hashseed + uint64(n))) * 0x9E3779B97F4A7C15) >> 56)
}
