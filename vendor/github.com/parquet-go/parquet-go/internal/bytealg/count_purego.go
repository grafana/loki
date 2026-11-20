//go:build !amd64 || (purego && !goexperiment.simd)

package bytealg

import "bytes"

func Count(data []byte, value byte) int {
	return bytes.Count(data, []byte{value})
}
