//go:build goexperiment.simd

package parquet

import (
	"simd/archsimd"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/internal/bytealg"
	"github.com/parquet-go/parquet-go/sparse"
)

// This file provides implementations of the column buffer functions based on
// the simd/archsimd package, replacing the hand-written assembly of
// column_buffer_amd64.s when GOEXPERIMENT=simd is set.

func broadcastValueInt32(dst []int32, src int8) {
	bytealg.Broadcast(unsafecast.Slice[byte](dst), byte(src))
}

var rangeIota8 = [8]int32{0, 1, 2, 3, 4, 5, 6, 7}

func broadcastRangeInt32(dst []int32, base int32) {
	i := 0
	if archsimd.X86.AVX2() && len(dst) >= 8 {
		v := archsimd.BroadcastInt32x8(base).Add(archsimd.LoadInt32x8Slice(rangeIota8[:]))
		step := archsimd.BroadcastInt32x8(8)
		for ; i+8 <= len(dst); i += 8 {
			v.StoreSlice(dst[i:])
			v = v.Add(step)
		}
		archsimd.ClearAVXUpperBits()
	}
	for ; i < len(dst); i++ {
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
