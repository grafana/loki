//go:build !amd64 || (purego && !goexperiment.simd)

package bytealg

func Broadcast(dst []byte, src byte) {
	for i := range dst {
		dst[i] = src
	}
}
