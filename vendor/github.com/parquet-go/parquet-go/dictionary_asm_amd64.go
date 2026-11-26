//go:build !purego && !goexperiment.simd

package parquet

//go:noescape
func dictionaryBoundsBE128(dict [][16]byte, indexes []int32) (min, max *[16]byte, err errno)
