//go:build !amd64 || purego

package parquet

import "github.com/parquet-go/parquet-go/sparse"

func broadcastValueInt32(dst []int32, src int8) {
	value := 0x01010101 * int32(src)
	for i := range dst {
		dst[i] = value
	}
}

func broadcastRangeInt32(dst []int32, base int32) {
	for i := range dst {
		dst[i] = base + int32(i)
	}
}

func writePointersBE128(values [][16]byte, rows sparse.Array) {
	for i := range values {
		p := *(**[16]byte)(rows.Index(i))

		if p != nil {
			values[i] = *p
		} else {
			values[i] = [16]byte{}
		}
	}
}
