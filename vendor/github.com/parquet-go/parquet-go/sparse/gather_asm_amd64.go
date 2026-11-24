//go:build !purego && !goexperiment.simd

package sparse

//go:noescape
func gatherBitsDefault(dst []byte, src Uint8Array)

//go:noescape
func gather128(dst [][16]byte, src Uint128Array) int
