// Package bloom implements parquet bloom filters.
package bloom

func fasthash1x64(value uint64, scale int32) uint64 {
	return ((value >> 32) * uint64(scale)) >> 32
}

func fasthash4x64(dst, src *[4]uint64, scale int32) {
	dst[0] = ((src[0] >> 32) * uint64(scale)) >> 32
	dst[1] = ((src[1] >> 32) * uint64(scale)) >> 32
	dst[2] = ((src[2] >> 32) * uint64(scale)) >> 32
	dst[3] = ((src[3] >> 32) * uint64(scale)) >> 32
}
