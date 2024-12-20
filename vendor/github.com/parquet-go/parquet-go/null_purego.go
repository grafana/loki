//go:build purego || !amd64

package parquet

import "github.com/parquet-go/parquet-go/sparse"

func nullIndexBool(bits []uint64, rows sparse.Array) {
	nullIndex[bool](bits, rows)
}

func nullIndexInt(bits []uint64, rows sparse.Array) {
	nullIndex[int](bits, rows)
}

func nullIndexInt32(bits []uint64, rows sparse.Array) {
	nullIndex[int32](bits, rows)
}

func nullIndexInt64(bits []uint64, rows sparse.Array) {
	nullIndex[int64](bits, rows)
}

func nullIndexUint(bits []uint64, rows sparse.Array) {
	nullIndex[uint](bits, rows)
}

func nullIndexUint32(bits []uint64, rows sparse.Array) {
	nullIndex[uint32](bits, rows)
}

func nullIndexUint64(bits []uint64, rows sparse.Array) {
	nullIndex[uint64](bits, rows)
}

func nullIndexUint128(bits []uint64, rows sparse.Array) {
	nullIndex[[16]byte](bits, rows)
}

func nullIndexFloat32(bits []uint64, rows sparse.Array) {
	nullIndex[float32](bits, rows)
}

func nullIndexFloat64(bits []uint64, rows sparse.Array) {
	nullIndex[float64](bits, rows)
}

func nullIndexString(bits []uint64, rows sparse.Array) {
	nullIndex[string](bits, rows)
}

func nullIndexSlice(bits []uint64, rows sparse.Array) {
	for i := 0; i < rows.Len(); i++ {
		p := *(**struct{})(rows.Index(i))
		b := uint64(0)
		if p != nil {
			b = 1
		}
		bits[uint(i)/64] |= b << (uint(i) % 64)
	}
}

func nullIndexPointer(bits []uint64, rows sparse.Array) {
	nullIndex[*struct{}](bits, rows)
}
