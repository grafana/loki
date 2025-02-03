// Package aeshash implements hashing functions derived from the Go runtime's
// internal hashing based on the support of AES encryption in CPU instructions.
//
// On architecture where the CPU does not provide instructions for AES
// encryption, the aeshash.Enabled function always returns false, and attempting
// to call any other function will trigger a panic.
package aeshash

import "github.com/parquet-go/parquet-go/sparse"

func MultiHash32(hashes []uintptr, values []uint32, seed uintptr) {
	MultiHashUint32Array(hashes, sparse.MakeUint32Array(values), seed)
}

func MultiHash64(hashes []uintptr, values []uint64, seed uintptr) {
	MultiHashUint64Array(hashes, sparse.MakeUint64Array(values), seed)
}

func MultiHash128(hashes []uintptr, values [][16]byte, seed uintptr) {
	MultiHashUint128Array(hashes, sparse.MakeUint128Array(values), seed)
}
