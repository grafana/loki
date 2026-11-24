//go:build !purego && goexperiment.simd

package parquet

// The assembly version of this kernel is a scalar BSWAP loop; the Go
// implementation below compiles to comparable code, so the GOEXPERIMENT=simd
// build uses it instead of the assembly. The other dictionary kernels keep
// their assembly (k-masked gathers and scatters have no archsimd equivalent).
func dictionaryBoundsBE128(dict [][16]byte, indexes []int32) (min, max *[16]byte, err errno) {
	for _, i := range indexes {
		if uint(i) >= uint(len(dict)) {
			return nil, nil, indexOutOfBounds
		}
		v := &dict[i]
		switch {
		case min == nil:
			min, max = v, v
		case lessBE128(v, min):
			min = v
		case lessBE128(max, v):
			max = v
		}
	}
	return min, max, ok
}
