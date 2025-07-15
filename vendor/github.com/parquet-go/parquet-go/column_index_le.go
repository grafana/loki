// This file gets added on all the little-endian CPU architectures.

//go:build 386 || amd64 || amd64p32 || alpha || arm || arm64 || loong64 || mipsle || mips64le || mips64p32le || nios2 || ppc64le || riscv || riscv64 || sh || wasm

package parquet

import (
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

func columnIndexInt32Values(values []int32) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexInt64Values(values []int64) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexInt96Values(values []deprecated.Int96) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexFloatValues(values []float32) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexDoubleValues(values []float64) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexUint32Values(values []uint32) []byte {
	return unsafecast.Slice[byte](values)
}

func columnIndexUint64Values(values []uint64) []byte {
	return unsafecast.Slice[byte](values)
}
