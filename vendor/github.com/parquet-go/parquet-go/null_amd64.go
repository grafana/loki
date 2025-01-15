//go:build !purego

package parquet

import "github.com/parquet-go/parquet-go/sparse"

//go:noescape
func nullIndex8(bits *uint64, rows sparse.Array)

//go:noescape
func nullIndex32(bits *uint64, rows sparse.Array)

//go:noescape
func nullIndex64(bits *uint64, rows sparse.Array)

//go:noescape
func nullIndex128(bits *uint64, rows sparse.Array)

func nullIndexBool(bits []uint64, rows sparse.Array) {
	nullIndex8(&bits[0], rows)
}

func nullIndexInt(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}

func nullIndexInt32(bits []uint64, rows sparse.Array) {
	nullIndex32(&bits[0], rows)
}

func nullIndexInt64(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}

func nullIndexUint(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}

func nullIndexUint32(bits []uint64, rows sparse.Array) {
	nullIndex32(&bits[0], rows)
}

func nullIndexUint64(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}

func nullIndexUint128(bits []uint64, rows sparse.Array) {
	nullIndex128(&bits[0], rows)
}

func nullIndexFloat32(bits []uint64, rows sparse.Array) {
	nullIndex32(&bits[0], rows)
}

func nullIndexFloat64(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}

func nullIndexString(bits []uint64, rows sparse.Array) {
	// We offset by an extra 8 bytes to test the lengths of string values where
	// the first field is the pointer and the second is the length which we want
	// to test.
	nullIndex64(&bits[0], rows.Offset(8))
}

func nullIndexSlice(bits []uint64, rows sparse.Array) {
	// Slice values are null if their pointer is nil, which is held in the first
	// 8 bytes of the object so we can simply test 64 bits words.
	nullIndex64(&bits[0], rows)
}

func nullIndexPointer(bits []uint64, rows sparse.Array) {
	nullIndex64(&bits[0], rows)
}
