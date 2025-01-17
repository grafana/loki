//go:build !purego

package parquet

import (
	"github.com/parquet-go/parquet-go/internal/bytealg"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
	"golang.org/x/sys/cpu"
)

func broadcastValueInt32(dst []int32, src int8) {
	bytealg.Broadcast(unsafecast.Slice[byte](dst), byte(src))
}

//go:noescape
func broadcastRangeInt32AVX2(dst []int32, base int32)

func broadcastRangeInt32(dst []int32, base int32) {
	if len(dst) >= 8 && cpu.X86.HasAVX2 {
		broadcastRangeInt32AVX2(dst, base)
	} else {
		for i := range dst {
			dst[i] = base + int32(i)
		}
	}
}

//go:noescape
func writePointersBE128(values [][16]byte, rows sparse.Array)
