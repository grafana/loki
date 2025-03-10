// This file gets added on all the big-endian CPU architectures.

//go:build armbe || arm64be || m68k || mips || mips64 || mips64p32 || ppc || ppc64 || s390 || s390x || shbe || sparc || sparc64

package parquet

import (
	"encoding/binary"
	"math"

	"github.com/parquet-go/parquet-go/deprecated"
)

func columnIndexInt32Values(values []int32) []byte {
	buf := make([]byte, len(values)*4)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint32(buf[idx:(4+idx)], uint32(values[k]))
		idx += 4
	}
	return buf
}

func columnIndexInt64Values(values []int64) []byte {
	buf := make([]byte, len(values)*8)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint64(buf[idx:(8+idx)], uint64(values[k]))
		idx += 8
	}
	return buf
}

func columnIndexInt96Values(values []deprecated.Int96) []byte {
	buf := make([]byte, len(values)*12)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint32(buf[idx:(4+idx)], uint32(values[k][0]))
		binary.LittleEndian.PutUint32(buf[(4+idx):(8+idx)], uint32(values[k][1]))
		binary.LittleEndian.PutUint32(buf[(8+idx):(12+idx)], uint32(values[k][2]))
		idx += 12
	}
	return buf
}

func columnIndexFloatValues(values []float32) []byte {
	buf := make([]byte, len(values)*4)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint32(buf[idx:(4+idx)], math.Float32bits(values[k]))
		idx += 4
	}
	return buf
}

func columnIndexDoubleValues(values []float64) []byte {
	buf := make([]byte, len(values)*8)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint64(buf[idx:(8+idx)], math.Float64bits(values[k]))
		idx += 8
	}
	return buf
}

func columnIndexUint32Values(values []uint32) []byte {
	buf := make([]byte, len(values)*4)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint32(buf[idx:(4+idx)], values[k])
		idx += 4
	}
	return buf
}

func columnIndexUint64Values(values []uint64) []byte {
	buf := make([]byte, len(values)*8)
	idx := 0
	for k := range len(values) {
		binary.LittleEndian.PutUint64(buf[idx:(8+idx)], values[k])
		idx += 8
	}
	return buf
}
