//go:build !purego

package wyhash

import "github.com/parquet-go/parquet-go/sparse"

//go:noescape
func MultiHashUint32Array(hashes []uintptr, values sparse.Uint32Array, seed uintptr)

//go:noescape
func MultiHashUint64Array(hashes []uintptr, values sparse.Uint64Array, seed uintptr)

//go:noescape
func MultiHashUint128Array(hashes []uintptr, values sparse.Uint128Array, seed uintptr)
