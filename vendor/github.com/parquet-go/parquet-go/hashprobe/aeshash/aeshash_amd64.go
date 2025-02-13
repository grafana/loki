//go:build !purego

package aeshash

import (
	"math/rand"
	"unsafe"

	"golang.org/x/sys/cpu"

	"github.com/parquet-go/parquet-go/sparse"
)

// hashRandomBytes is 48 since this is what the assembly code depends on.
const hashRandomBytes = 48

var aeskeysched [hashRandomBytes]byte

func init() {
	for _, v := range aeskeysched {
		if v != 0 {
			// aeskeysched was initialized somewhere else (e.g. tests), so we
			// can skip initialization. No synchronization is needed since init
			// functions are called sequentially in a single goroutine (see
			// https://go.dev/ref/spec#Package_initialization).
			return
		}
	}

	key := (*[hashRandomBytes / 8]uint64)(unsafe.Pointer(&aeskeysched))
	for i := range key {
		key[i] = rand.Uint64()
	}
}

// Enabled returns true if AES hash is available on the system.
//
// The function uses the same logic than the Go runtime since we depend on
// the AES hash state being initialized.
//
// See https://go.dev/src/runtime/alg.go
func Enabled() bool { return cpu.X86.HasAES && cpu.X86.HasSSSE3 && cpu.X86.HasSSE41 }

//go:noescape
func Hash32(value uint32, seed uintptr) uintptr

//go:noescape
func Hash64(value uint64, seed uintptr) uintptr

//go:noescape
func Hash128(value [16]byte, seed uintptr) uintptr

//go:noescape
func MultiHashUint32Array(hashes []uintptr, values sparse.Uint32Array, seed uintptr)

//go:noescape
func MultiHashUint64Array(hashes []uintptr, values sparse.Uint64Array, seed uintptr)

//go:noescape
func MultiHashUint128Array(hashes []uintptr, values sparse.Uint128Array, seed uintptr)
