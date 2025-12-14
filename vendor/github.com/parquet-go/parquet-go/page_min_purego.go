//go:build purego || !amd64

package parquet

import (
	"encoding/binary"
	"slices"
)

func minInt32(data []int32) int32 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minInt64(data []int64) int64 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minUint32(data []uint32) uint32 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minUint64(data []uint64) uint64 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minFloat32(data []float32) float32 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minFloat64(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	return slices.Min(data)
}

func minBE128(data [][16]byte) (min []byte) {
	if len(data) > 0 {
		m := binary.BigEndian.Uint64(data[0][:8])
		j := 0
		for i := 1; i < len(data); i++ {
			x := binary.BigEndian.Uint64(data[i][:8])
			switch {
			case x < m:
				m, j = x, i
			case x == m:
				y := binary.BigEndian.Uint64(data[i][8:])
				n := binary.BigEndian.Uint64(data[j][8:])
				if y < n {
					m, j = x, i
				}
			}
		}
		min = data[j][:]
	}
	return min
}
