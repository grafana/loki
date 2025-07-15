//go:build purego || !amd64

package aeshash

import "github.com/parquet-go/parquet-go/sparse"

// Enabled always returns false since we assume that AES instructions are not
// available by default.
func Enabled() bool { return false }

const unsupported = "BUG: AES hash is not available on this platform"

func Hash32(value uint32, seed uintptr) uintptr { panic(unsupported) }

func Hash64(value uint64, seed uintptr) uintptr { panic(unsupported) }

func Hash128(value [16]byte, seed uintptr) uintptr { panic(unsupported) }

func MultiHashUint32Array(hashes []uintptr, values sparse.Uint32Array, seed uintptr) {
	panic(unsupported)
}

func MultiHashUint64Array(hashes []uintptr, values sparse.Uint64Array, seed uintptr) {
	panic(unsupported)
}

func MultiHashUint128Array(hashes []uintptr, values sparse.Uint128Array, seed uintptr) {
	panic(unsupported)
}
