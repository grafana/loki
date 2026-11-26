//go:build !amd64 || !goexperiment.simd

package delta

func searchPrefixLength(base, data []byte) int {
	return wordSearchPrefixLength(base, data)
}
