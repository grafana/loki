//go:build !purego && !goexperiment.simd

package parquet

import "github.com/parquet-go/parquet-go/sparse"

//go:noescape
func nullIndex8(bits *uint64, rows sparse.Array)

func nullIndex32(bits *uint64, rows sparse.Array) {
	nullIndexGather32(bits, rows)
}

func nullIndex64(bits *uint64, rows sparse.Array) {
	nullIndexGather64(bits, rows)
}

//go:noescape
func nullIndex128(bits *uint64, rows sparse.Array)
