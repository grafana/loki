//go:build purego || !amd64

package hashprobe

import (
	"github.com/parquet-go/parquet-go/sparse"
)

func multiProbe32(table []table32Group, numKeys int, hashes []uintptr, keys sparse.Uint32Array, values []int32) int {
	return multiProbe32Default(table, numKeys, hashes, keys, values)
}

func multiProbe64(table []table64Group, numKeys int, hashes []uintptr, keys sparse.Uint64Array, values []int32) int {
	return multiProbe64Default(table, numKeys, hashes, keys, values)
}

func multiProbe128(table []byte, tableCap, tableLen int, hashes []uintptr, keys sparse.Uint128Array, values []int32) int {
	return multiProbe128Default(table, tableCap, tableLen, hashes, keys, values)
}
