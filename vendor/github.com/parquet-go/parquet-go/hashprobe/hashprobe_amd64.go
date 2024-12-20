//go:build !purego

package hashprobe

import (
	"github.com/parquet-go/parquet-go/sparse"
	"golang.org/x/sys/cpu"
)

//go:noescape
func multiProbe32AVX2(table []table32Group, numKeys int, hashes []uintptr, keys sparse.Uint32Array, values []int32) int

//go:noescape
func multiProbe64AVX2(table []table64Group, numKeys int, hashes []uintptr, keys sparse.Uint64Array, values []int32) int

//go:noescape
func multiProbe128SSE2(table []byte, tableCap, tableLen int, hashes []uintptr, keys sparse.Uint128Array, values []int32) int

func multiProbe32(table []table32Group, numKeys int, hashes []uintptr, keys sparse.Uint32Array, values []int32) int {
	if cpu.X86.HasAVX2 {
		return multiProbe32AVX2(table, numKeys, hashes, keys, values)
	}
	return multiProbe32Default(table, numKeys, hashes, keys, values)
}

func multiProbe64(table []table64Group, numKeys int, hashes []uintptr, keys sparse.Uint64Array, values []int32) int {
	if cpu.X86.HasAVX2 {
		return multiProbe64AVX2(table, numKeys, hashes, keys, values)
	}
	return multiProbe64Default(table, numKeys, hashes, keys, values)
}

func multiProbe128(table []byte, tableCap, tableLen int, hashes []uintptr, keys sparse.Uint128Array, values []int32) int {
	if cpu.X86.HasSSE2 {
		return multiProbe128SSE2(table, tableCap, tableLen, hashes, keys, values)
	}
	return multiProbe128Default(table, tableCap, tableLen, hashes, keys, values)
}
