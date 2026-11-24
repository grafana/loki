//go:build !purego && !goexperiment.simd

package parquet

import "github.com/parquet-go/parquet-go/sparse"

// The VPGATHER based null index kernels, shared by both the assembly build
// (as the whole implementation) and the GOEXPERIMENT=simd build (for
// strided rows, where hardware gathers beat plain loads on cache resident
// data).

//go:noescape
func nullIndexGather32(bits *uint64, rows sparse.Array)

//go:noescape
func nullIndexGather64(bits *uint64, rows sparse.Array)
