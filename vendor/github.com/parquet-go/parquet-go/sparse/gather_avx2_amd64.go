//go:build !purego && !goexperiment.simd

package sparse

// The VPGATHER kernels, shared by the assembly build (dispatched from
// gather_amd64.go) and the GOEXPERIMENT=simd build (used for strided input,
// where hardware gathers beat plain loads on cache resident data).

//go:noescape
func gatherBitsAVX2(dst []byte, src Uint8Array)

//go:noescape
func gather32AVX2(dst []uint32, src Uint32Array)

//go:noescape
func gather64AVX2(dst []uint64, src Uint64Array)
